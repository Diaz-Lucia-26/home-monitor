package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"home-monitor/internal/capture"
	"home-monitor/internal/config"
	"home-monitor/internal/handler"
	"home-monitor/internal/monitor"
	"home-monitor/internal/rtmp"
	"home-monitor/internal/storage"
	"home-monitor/internal/stream"
	"home-monitor/internal/webrtc"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 创建必要的目录
	if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
		log.Fatalf("创建存储目录失败: %v", err)
	}
	if err := os.MkdirAll(cfg.Stream.TempPath, 0755); err != nil {
		log.Fatalf("创建临时目录失败: %v", err)
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化采集器管理器（统一的音视频采集）
	captureManager := capture.NewManager()

	// 初始化流管理器和存储管理器（使用采集器）
	streamManager := stream.NewStreamManager(captureManager, cfg.Stream)
	storageManager := storage.NewStorageManager(captureManager, cfg.Storage)

	// 添加采集器（每个摄像头一个）
	for _, camCfg := range cfg.Cameras {
		if !camCfg.Enabled {
			continue
		}

		// 如果启用录像，使用带录制配置的采集器
		if cfg.Storage.Enabled {
			recCfg := capture.RecordingConfig{
				OutputPath:      cfg.Storage.Path,
				SegmentDuration: cfg.Storage.GetSegmentDurationSeconds(),
				Format:          cfg.Storage.Format,
			}
			if _, err := captureManager.AddCapturerWithRecording(camCfg, recCfg); err != nil {
				log.Printf("添加采集器 %s 失败: %v", camCfg.ID, err)
				continue
			}
		} else {
			if _, err := captureManager.AddCapturer(camCfg); err != nil {
				log.Printf("添加采集器 %s 失败: %v", camCfg.ID, err)
				continue
			}
		}
	}

	// 启动所有采集器
	if err := captureManager.StartAll(ctx); err != nil {
		log.Printf("启动采集器失败: %v", err)
	}

	// 启动流处理（HLS、MJPEG 分发）
	if err := streamManager.StartAll(ctx); err != nil {
		log.Printf("启动流处理失败: %v", err)
	}

	// 录像功能由 FFmpeg segment 自动处理（在 capturer 启动时已经开始）
	if cfg.Storage.Enabled {
		log.Println("📹 录像功能已启用（FFmpeg segment 自动分段）")
	}

	// 启动清理任务
	go storageManager.StartCleanupTask(ctx)

	// 启动性能监控
	perfMonitor := monitor.NewMonitor()
	perfMonitor.SetThresholds(512, 1000) // 内存 512MB, Goroutine 1000
	perfMonitor.Start(ctx)

	// 设置 Gin
	gin.SetMode(gin.ReleaseMode)

	// 服务器列表
	var servers []*http.Server
	var webrtcServer *webrtc.Server
	var rtmpManager *rtmp.Manager

	// 创建 RTMP 管理器
	rtmpManager = rtmp.NewManager(ctx, captureManager, cfg.Cameras)

	// 创建 HLS 输出管理器
	hlsOutputManager := stream.NewHLSOutputManager(ctx, captureManager, cfg.Cameras, cfg.Stream)

	// ===== 主服务（管理后台） =====
	mainRouter := gin.Default()
	mainRouter.Use(corsMiddleware()) // 允许跨域访问（供 MJPEG/WebRTC 独立前端调用 API）

	h := handler.NewHandler(captureManager, streamManager, storageManager)

	// 设置预览服务配置（用于主页显示链接）
	h.SetPreviewConfig(&handler.PreviewDisplayConfig{
		Host:          cfg.Server.Host,
		MJPEGEnabled:  cfg.Preview.MJPEG.Enabled,
		MJPEGPort:     cfg.Preview.MJPEG.Port,
		WebRTCEnabled: cfg.Preview.WebRTC.Enabled,
		WebRTCPort:    cfg.Preview.WebRTC.Port,
	})

	handler.SetupRoutes(mainRouter, h, nil) // 主服务不需要 WebRTC handler

	// 注册 RTMP API 路由
	rtmpHandler := handler.NewRTMPHandler(rtmpManager)
	rtmpHandler.RegisterRoutes(mainRouter.Group("/api"))

	// 注册 HLS 输出 API 路由
	hlsHandler := handler.NewHLSHandler(hlsOutputManager)
	hlsAPI := mainRouter.Group("/api/hls")
	{
		hlsAPI.POST("/:camera_id/start", hlsHandler.StartHLSOutput)
		hlsAPI.POST("/:camera_id/stop", hlsHandler.StopHLSOutput)
		hlsAPI.GET("/:camera_id/status", hlsHandler.GetHLSStatus)
		hlsAPI.GET("/status", hlsHandler.GetAllHLSStatus)
	}

	// 提供 HLS 分片文件服务
	mainRouter.Static("/hls", hlsOutputManager.GetOutputPath())

	// 注册性能监控 API 路由
	monitorHandler := handler.NewMonitorHandler(perfMonitor)
	monitorHandler.RegisterRoutes(mainRouter.Group("/api"))

	mainAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	mainServer := &http.Server{
		Addr:    mainAddr,
		Handler: mainRouter,
	}
	servers = append(servers, mainServer)

	go func() {
		log.Println("🏠 家庭监控服务已启动")
		log.Printf("📺 主控制台: http://%s", mainAddr)
		log.Printf("📁 录像存储: %s", cfg.Storage.Path)

		if err := mainServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("主服务器启动失败: %v", err)
		}
	}()

	// ===== MJPEG 独立服务 =====
	if cfg.Preview.MJPEG.Enabled {
		mjpegRouter := gin.New()
		mjpegRouter.Use(gin.Recovery())
		mjpegRouter.Use(corsMiddleware()) // 允许跨域

		mjpegHandler := handler.NewMJPEGHandler(
			captureManager,
			cfg.Preview.MJPEG.Quality,
			cfg.Server.Port,
			cfg.Preview.MJPEG.Port,
		)
		handler.SetupMJPEGRoutes(mjpegRouter, mjpegHandler)

		mjpegAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Preview.MJPEG.Port)
		mjpegServer := &http.Server{
			Addr:    mjpegAddr,
			Handler: mjpegRouter,
		}
		servers = append(servers, mjpegServer)

		go func() {
			log.Printf("📺 MJPEG 服务: http://%s", mjpegAddr)
			if err := mjpegServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("MJPEG 服务器启动失败: %v", err)
			}
		}()
	}

	// ===== WebRTC 独立服务 =====
	if cfg.Preview.WebRTC.Enabled {
		webrtcRouter := gin.New()
		webrtcRouter.Use(gin.Recovery())
		webrtcRouter.Use(corsMiddleware()) // 允许跨域

		webrtcServer = webrtc.NewServer(captureManager, cfg.Cameras, cfg.Preview.WebRTC.STUNServer)
		webrtcHandler := handler.NewWebRTCHandler(
			webrtcServer,
			cfg.Server.Port,
			cfg.Preview.WebRTC.Port,
		)
		handler.SetupWebRTCRoutes(webrtcRouter, webrtcHandler)

		webrtcAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Preview.WebRTC.Port)
		webrtcHttpServer := &http.Server{
			Addr:    webrtcAddr,
			Handler: webrtcRouter,
		}
		servers = append(servers, webrtcHttpServer)

		go func() {
			log.Printf("🌐 WebRTC 服务: http://%s", webrtcAddr)
			if err := webrtcHttpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("WebRTC 服务器启动失败: %v", err)
			}
		}()
	}

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("正在关闭服务...")

	// 优雅关闭
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// 停止所有组件
	perfMonitor.Stop()         // 先停监控
	hlsOutputManager.StopAll() // 停 HLS
	captureManager.StopAll()
	streamManager.StopAll()
	storageManager.StopAll()
	if webrtcServer != nil {
		webrtcServer.CloseAll()
	}
	if rtmpManager != nil {
		rtmpManager.StopAll()
	}
	cancel()

	// 关闭所有服务器
	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("关闭服务器失败: %v", err)
		}
	}

	log.Println("服务已关闭")
}

// corsMiddleware 跨域中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
