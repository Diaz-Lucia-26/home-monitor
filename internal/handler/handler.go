package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"home-monitor/internal/capture"
	"home-monitor/internal/storage"
	"home-monitor/internal/stream"
)

// Handler HTTP处理器
type Handler struct {
	captureManager *capture.Manager
	streamManager  *stream.StreamManager
	storageManager *storage.StorageManager
	upgrader       websocket.Upgrader
	// 预览服务配置（用于主页显示链接）
	previewConfig *PreviewDisplayConfig
}

// PreviewDisplayConfig 预览显示配置
type PreviewDisplayConfig struct {
	Host          string
	MJPEGEnabled  bool
	MJPEGPort     int
	WebRTCEnabled bool
	WebRTCPort    int
}

// NewHandler 创建处理器
func NewHandler(capManager *capture.Manager, streamManager *stream.StreamManager, storageManager *storage.StorageManager) *Handler {
	return &Handler{
		captureManager: capManager,
		streamManager:  streamManager,
		storageManager: storageManager,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

// SetPreviewConfig 设置预览配置
func (h *Handler) SetPreviewConfig(cfg *PreviewDisplayConfig) {
	h.previewConfig = cfg
}

// CameraInfo 摄像头信息
type CameraInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsRunning bool   `json:"is_running"`
	HasAudio  bool   `json:"has_audio"`
}

// GetCameras 获取所有摄像头
func (h *Handler) GetCameras(c *gin.Context) {
	capturers := h.captureManager.GetAllCapturers()
	var infos []CameraInfo
	for _, cap := range capturers {
		infos = append(infos, CameraInfo{
			ID:        cap.GetID(),
			Name:      cap.GetName(),
			IsRunning: cap.IsRunning(),
			HasAudio:  cap.HasAudio(),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    infos,
	})
}

// GetCamera 获取单个摄像头
func (h *Handler) GetCamera(c *gin.Context) {
	id := c.Param("id")
	cap, err := h.captureManager.GetCapturer(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": CameraInfo{
			ID:        cap.GetID(),
			Name:      cap.GetName(),
			IsRunning: cap.IsRunning(),
			HasAudio:  cap.HasAudio(),
		},
	})
}

// StreamMJPEG MJPEG流
func (h *Handler) StreamMJPEG(c *gin.Context) {
	id := c.Param("id")
	cap, err := h.captureManager.GetCapturer(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	if !cap.IsRunning() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "摄像头未运行",
		})
		return
	}

	c.Header("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 订阅帧通道
	subID := fmt.Sprintf("mjpeg_%d", time.Now().UnixNano())
	frameChannel := cap.SubscribeFrames(subID)
	defer cap.UnsubscribeFrames(subID)

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case frame, ok := <-frameChannel:
			if !ok {
				return
			}
			c.Writer.Write([]byte("--frame\r\n"))
			c.Writer.Write([]byte("Content-Type: image/jpeg\r\n"))
			c.Writer.Write([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(frame))))
			c.Writer.Write(frame)
			c.Writer.Write([]byte("\r\n"))
			c.Writer.Flush()
		}
	}
}

// StreamWebSocket WebSocket流
func (h *Handler) StreamWebSocket(c *gin.Context) {
	id := c.Param("id")
	cap, err := h.captureManager.GetCapturer(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "WebSocket升级失败",
		})
		return
	}
	defer conn.Close()

	if !cap.IsRunning() {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"error": "摄像头未运行"}`))
		return
	}

	// 订阅帧通道
	subID := fmt.Sprintf("websocket_%d", time.Now().UnixNano())
	frameChannel := cap.SubscribeFrames(subID)
	defer cap.UnsubscribeFrames(subID)

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case frame, ok := <-frameChannel:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return
			}
		}
	}
}

// GetSnapshot 获取快照
func (h *Handler) GetSnapshot(c *gin.Context) {
	id := c.Param("id")
	cap, err := h.captureManager.GetCapturer(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	frame, err := cap.GetFrame()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.Header("Content-Type", "image/jpeg")
	c.Writer.Write(frame)
}

// GetRecordings 获取录像列表
func (h *Handler) GetRecordings(c *gin.Context) {
	cameraID := c.Query("camera_id")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	if startTimeStr != "" {
		startTime, _ = time.Parse(time.RFC3339, startTimeStr)
	}
	if endTimeStr != "" {
		endTime, _ = time.Parse(time.RFC3339, endTimeStr)
	}

	var recordings []storage.Recording
	var err error

	if cameraID != "" {
		recordings, err = h.storageManager.GetRecordings(cameraID, startTime, endTime)
	} else {
		recordings, err = h.storageManager.GetAllRecordings()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    recordings,
	})
}

// DownloadRecording 下载录像
func (h *Handler) DownloadRecording(c *gin.Context) {
	cameraID := c.Param("camera_id")
	fileName := c.Param("filename")

	recordings, err := h.storageManager.GetRecordings(cameraID, time.Time{}, time.Time{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	for _, rec := range recordings {
		if rec.FileName == fileName {
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
			c.File(rec.FilePath)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{
		"success": false,
		"error":   "录像不存在",
	})
}

// PlayRecording 播放录像
func (h *Handler) PlayRecording(c *gin.Context) {
	cameraID := c.Param("camera_id")
	fileName := c.Param("filename")

	recordings, err := h.storageManager.GetRecordings(cameraID, time.Time{}, time.Time{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	for _, rec := range recordings {
		if rec.FileName == fileName {
			c.File(rec.FilePath)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{
		"success": false,
		"error":   "录像不存在",
	})
}

// DeleteRecording 删除录像
func (h *Handler) DeleteRecording(c *gin.Context) {
	cameraID := c.Param("camera_id")
	fileName := c.Param("filename")

	recordings, err := h.storageManager.GetRecordings(cameraID, time.Time{}, time.Time{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	for _, rec := range recordings {
		if rec.FileName == fileName {
			if err := h.storageManager.DeleteRecording(rec.FilePath); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "录像已删除",
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{
		"success": false,
		"error":   "录像不存在",
	})
}

// Index 首页
func (h *Handler) Index(c *gin.Context) {
	data := gin.H{
		"title": "家庭监控系统",
	}

	// 添加预览服务配置
	if h.previewConfig != nil {
		host := h.previewConfig.Host
		if host == "0.0.0.0" {
			host = c.Request.Host
			// 去掉端口部分
			if idx := len(host) - 1; idx > 0 {
				for i := len(host) - 1; i >= 0; i-- {
					if host[i] == ':' {
						host = host[:i]
						break
					}
				}
			}
		}
		data["Host"] = host
		data["MJPEGEnabled"] = h.previewConfig.MJPEGEnabled
		data["MJPEGPort"] = h.previewConfig.MJPEGPort
		data["WebRTCEnabled"] = h.previewConfig.WebRTCEnabled
		data["WebRTCPort"] = h.previewConfig.WebRTCPort
	}

	c.HTML(http.StatusOK, "index.html", data)
}

// RTMPPage RTMP 管理页面
func (h *Handler) RTMPPage(c *gin.Context) {
	c.HTML(http.StatusOK, "rtmp.html", gin.H{
		"title": "RTMP 推流管理",
	})
}

// HLSPage HLS 推流管理页面
func (h *Handler) HLSPage(c *gin.Context) {
	c.HTML(http.StatusOK, "hls.html", gin.H{
		"title": "HLS 推流管理",
	})
}

// MonitorPage 性能监控页面
func (h *Handler) MonitorPage(c *gin.Context) {
	c.HTML(http.StatusOK, "monitor.html", gin.H{
		"title": "性能监控",
	})
}

// SystemStatus 系统状态
func (h *Handler) SystemStatus(c *gin.Context) {
	capturers := h.captureManager.GetAllCapturers()
	runningCount := 0
	for _, cap := range capturers {
		if cap.IsRunning() {
			runningCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total_cameras":   len(capturers),
			"running_cameras": runningCount,
			"timestamp":       time.Now().Format(time.RFC3339),
		},
	})
}
