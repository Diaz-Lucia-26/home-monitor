package handler

import (
	"github.com/gin-gonic/gin"
)

// SetupRoutes 设置路由
func SetupRoutes(router *gin.Engine, handler *Handler, webrtcHandler *WebRTCHandler) {
	// 静态文件
	router.Static("/static", "./web/static")
	router.LoadHTMLGlob("./web/templates/*")

	// 首页
	router.GET("/", handler.Index)

	// RTMP 管理页面
	router.GET("/rtmp", handler.RTMPPage)

	// HLS 推流管理页面
	router.GET("/hls", handler.HLSPage)

	// 性能监控页面
	router.GET("/monitor", handler.MonitorPage)

	// API路由
	api := router.Group("/api")
	{
		// 系统
		api.GET("/status", handler.SystemStatus)

		// 摄像头
		cameras := api.Group("/cameras")
		{
			cameras.GET("", handler.GetCameras)
			cameras.GET("/:id", handler.GetCamera)
			cameras.GET("/:id/snapshot", handler.GetSnapshot)
		}

		// 流
		stream := api.Group("/stream")
		{
			stream.GET("/:id/mjpeg", handler.StreamMJPEG)
			stream.GET("/:id/ws", handler.StreamWebSocket)
		}

		// WebRTC
		if webrtcHandler != nil {
			webrtcGroup := api.Group("/webrtc")
			{
				webrtcGroup.POST("/offer", webrtcHandler.HandleOffer)
				webrtcGroup.POST("/ice-candidate", webrtcHandler.HandleICECandidate)
				webrtcGroup.DELETE("/connection/:connection_id", webrtcHandler.CloseConnection)
				webrtcGroup.GET("/status", webrtcHandler.GetStatus)
			}
		}

		// 录像
		recordings := api.Group("/recordings")
		{
			recordings.GET("", handler.GetRecordings)
			recordings.GET("/:camera_id/:filename", handler.PlayRecording)
			recordings.GET("/:camera_id/:filename/download", handler.DownloadRecording)
			recordings.DELETE("/:camera_id/:filename", handler.DeleteRecording)
		}
	}
}
