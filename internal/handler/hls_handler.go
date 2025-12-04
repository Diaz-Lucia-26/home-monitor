package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"home-monitor/internal/stream"
)

// HLSHandler HLS 输出处理器
type HLSHandler struct {
	hlsManager *stream.HLSOutputManager
}

// NewHLSHandler 创建 HLS 处理器
func NewHLSHandler(hlsManager *stream.HLSOutputManager) *HLSHandler {
	return &HLSHandler{
		hlsManager: hlsManager,
	}
}

// StartHLSOutput 启动 HLS 输出
func (h *HLSHandler) StartHLSOutput(c *gin.Context) {
	cameraID := c.Param("camera_id")
	if cameraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "camera_id 不能为空",
		})
		return
	}

	if err := h.hlsManager.StartOutput(cameraID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	running, url := h.hlsManager.GetOutputStatus(cameraID)
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  "HLS 输出已启动",
		"running":  running,
		"playlist": url,
	})
}

// StopHLSOutput 停止 HLS 输出
func (h *HLSHandler) StopHLSOutput(c *gin.Context) {
	cameraID := c.Param("camera_id")
	if cameraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "camera_id 不能为空",
		})
		return
	}

	if err := h.hlsManager.StopOutput(cameraID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "HLS 输出已停止",
	})
}

// GetHLSStatus 获取 HLS 输出状态
func (h *HLSHandler) GetHLSStatus(c *gin.Context) {
	cameraID := c.Param("camera_id")

	if cameraID != "" {
		running, url := h.hlsManager.GetOutputStatus(cameraID)
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"running":  running,
			"playlist": url,
		})
		return
	}

	// 获取所有输出状态
	outputs := h.hlsManager.GetAllOutputs()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"outputs": outputs,
	})
}

// GetAllHLSStatus 获取所有 HLS 输出状态
func (h *HLSHandler) GetAllHLSStatus(c *gin.Context) {
	outputs := h.hlsManager.GetAllOutputs()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"outputs": outputs,
	})
}
