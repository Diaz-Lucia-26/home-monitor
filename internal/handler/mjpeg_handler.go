package handler

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"home-monitor/internal/capture"
)

// MJPEGHandler MJPEG 独立服务处理器
type MJPEGHandler struct {
	capManager *capture.Manager
	quality    int
	mainPort   int
	mjpegPort  int
}

// NewMJPEGHandler 创建 MJPEG 处理器
func NewMJPEGHandler(capManager *capture.Manager, quality, mainPort, mjpegPort int) *MJPEGHandler {
	return &MJPEGHandler{
		capManager: capManager,
		quality:    quality,
		mainPort:   mainPort,
		mjpegPort:  mjpegPort,
	}
}

// Index MJPEG 服务首页
func (h *MJPEGHandler) Index(c *gin.Context) {
	tmpl, err := template.ParseFiles("./web/templates/mjpeg.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "加载模板失败: %v", err)
		return
	}

	tmpl.Execute(c.Writer, gin.H{
		"MainPort":  h.mainPort,
		"MJPEGPort": h.mjpegPort,
	})
}

// StreamMJPEG MJPEG 流
func (h *MJPEGHandler) StreamMJPEG(c *gin.Context) {
	cameraID := c.Param("id")

	capturer, err := h.capManager.GetCapturer(cameraID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "摄像头不存在"})
		return
	}

	// 设置 MJPEG 头
	c.Header("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 订阅帧 - 生成唯一订阅ID
	subID := fmt.Sprintf("mjpeg-%s-%d", cameraID, time.Now().UnixNano())
	frameCh := capturer.SubscribeFrames(subID)
	defer capturer.UnsubscribeFrames(subID)

	for {
		select {
		case frame, ok := <-frameCh:
			if !ok {
				return
			}

			// 写入 MJPEG 边界和帧
			fmt.Fprintf(c.Writer, "--frame\r\n")
			fmt.Fprintf(c.Writer, "Content-Type: image/jpeg\r\n")
			fmt.Fprintf(c.Writer, "Content-Length: %d\r\n\r\n", len(frame))
			c.Writer.Write(frame)
			fmt.Fprintf(c.Writer, "\r\n")
			c.Writer.Flush()

		case <-c.Request.Context().Done():
			return

		case <-time.After(10 * time.Second):
			// 超时，发送保活
			continue
		}
	}
}

// GetSnapshot 获取快照
func (h *MJPEGHandler) GetSnapshot(c *gin.Context) {
	cameraID := c.Param("id")

	capturer, err := h.capManager.GetCapturer(cameraID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "摄像头不存在"})
		return
	}

	frame, err := capturer.GetFrame()
	if err != nil || frame == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "暂无画面"})
		return
	}

	c.Data(http.StatusOK, "image/jpeg", frame)
}

// SetupMJPEGRoutes 设置 MJPEG 服务路由
func SetupMJPEGRoutes(router *gin.Engine, h *MJPEGHandler) {
	// 首页
	router.GET("/", h.Index)

	// MJPEG 流
	router.GET("/stream/:id/mjpeg", h.StreamMJPEG)

	// 快照
	router.GET("/snapshot/:id", h.GetSnapshot)
}
