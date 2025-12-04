package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"home-monitor/internal/rtmp"
)

// RTMPHandler RTMP 推流 API 处理器
type RTMPHandler struct {
	manager *rtmp.Manager
}

// NewRTMPHandler 创建 RTMP 处理器
func NewRTMPHandler(manager *rtmp.Manager) *RTMPHandler {
	return &RTMPHandler{manager: manager}
}

// StartStreamRequest 启动推流请求
type StartStreamRequest struct {
	CameraID string `json:"camera_id" binding:"required"`
	RTMPURL  string `json:"rtmp_url" binding:"required"`
}

// StartStream 启动 RTMP 推流
// POST /api/rtmp/start
func (h *RTMPHandler) StartStream(c *gin.Context) {
	var req StartStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.manager.StartStream(req.CameraID, req.RTMPURL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "RTMP 推流已启动",
		"camera_id": req.CameraID,
		"rtmp_url":  req.RTMPURL,
	})
}

// StopStreamRequest 停止推流请求
type StopStreamRequest struct {
	CameraID string `json:"camera_id" binding:"required"`
}

// StopStream 停止 RTMP 推流
// POST /api/rtmp/stop
func (h *RTMPHandler) StopStream(c *gin.Context) {
	var req StopStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.manager.StopStream(req.CameraID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "RTMP 推流已停止",
		"camera_id": req.CameraID,
	})
}

// GetStatus 获取推流状态
// GET /api/rtmp/status/:camera_id
func (h *RTMPHandler) GetStatus(c *gin.Context) {
	cameraID := c.Param("camera_id")
	if cameraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera_id 不能为空"})
		return
	}

	running, url := h.manager.GetStreamStatus(cameraID)
	c.JSON(http.StatusOK, gin.H{
		"camera_id": cameraID,
		"running":   running,
		"rtmp_url":  url,
	})
}

// GetAllStreams 获取所有推流
// GET /api/rtmp/streams
func (h *RTMPHandler) GetAllStreams(c *gin.Context) {
	streams := h.manager.GetAllStreams()
	c.JSON(http.StatusOK, gin.H{
		"streams": streams,
	})
}

// RegisterRoutes 注册路由
func (h *RTMPHandler) RegisterRoutes(r *gin.RouterGroup) {
	rtmpGroup := r.Group("/rtmp")
	{
		rtmpGroup.POST("/start", h.StartStream)
		rtmpGroup.POST("/stop", h.StopStream)
		rtmpGroup.GET("/status/:camera_id", h.GetStatus)
		rtmpGroup.GET("/streams", h.GetAllStreams)
	}
}
