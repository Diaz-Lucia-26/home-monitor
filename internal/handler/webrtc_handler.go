package handler

import (
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"

	"home-monitor/internal/webrtc"
)

// WebRTCHandler WebRTC 处理器
type WebRTCHandler struct {
	webrtcServer *webrtc.Server
	mainPort     int
	webrtcPort   int
}

// NewWebRTCHandler 创建 WebRTC 处理器
func NewWebRTCHandler(server *webrtc.Server, mainPort, webrtcPort int) *WebRTCHandler {
	return &WebRTCHandler{
		webrtcServer: server,
		mainPort:     mainPort,
		webrtcPort:   webrtcPort,
	}
}

// Index WebRTC 服务首页
func (h *WebRTCHandler) Index(c *gin.Context) {
	tmpl, err := template.ParseFiles("./web/templates/webrtc.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "加载模板失败: %v", err)
		return
	}

	tmpl.Execute(c.Writer, gin.H{
		"MainPort":   h.mainPort,
		"WebRTCPort": h.webrtcPort,
	})
}

// HandleOffer 处理 WebRTC Offer
func (h *WebRTCHandler) HandleOffer(c *gin.Context) {
	var req webrtc.OfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "无效的请求参数",
		})
		return
	}

	if req.CameraID == "" || req.SDP == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "缺少必要参数",
		})
		return
	}

	answerSDP, connID, err := h.webrtcServer.HandleOffer(c.Request.Context(), req.CameraID, req.SDP)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": webrtc.AnswerResponse{
			SDP:          answerSDP,
			ConnectionID: connID,
		},
	})
}

// HandleICECandidate 处理 ICE 候选
func (h *WebRTCHandler) HandleICECandidate(c *gin.Context) {
	var req webrtc.ICECandidateMessage
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "无效的请求参数",
		})
		return
	}

	if err := h.webrtcServer.AddICECandidate(req.ConnectionID, req.Candidate); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// CloseConnectionRequest 关闭连接请求
type CloseConnectionRequest struct {
	ConnectionID string `json:"connection_id"`
}

// CloseConnection 关闭 WebRTC 连接 (DELETE)
func (h *WebRTCHandler) CloseConnection(c *gin.Context) {
	connID := c.Param("connection_id")
	if connID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "缺少连接ID",
		})
		return
	}

	if err := h.webrtcServer.CloseConnection(connID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// CloseConnectionPost 关闭 WebRTC 连接 (POST)
func (h *WebRTCHandler) CloseConnectionPost(c *gin.Context) {
	var req CloseConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "无效的请求参数",
		})
		return
	}

	if req.ConnectionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "缺少连接ID",
		})
		return
	}

	if err := h.webrtcServer.CloseConnection(req.ConnectionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// GetStatus 获取 WebRTC 状态
func (h *WebRTCHandler) GetStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"connections": h.webrtcServer.GetConnectionCount(),
		},
	})
}

// SetupWebRTCRoutes 设置 WebRTC 独立服务路由
func SetupWebRTCRoutes(router *gin.Engine, h *WebRTCHandler) {
	// 首页
	router.GET("/", h.Index)

	// WebRTC API (放在根路径，方便跨域调用)
	router.POST("/webrtc/offer", h.HandleOffer)
	router.POST("/webrtc/ice-candidate", h.HandleICECandidate)
	router.POST("/webrtc/close", h.CloseConnectionPost)
	router.DELETE("/webrtc/connection/:connection_id", h.CloseConnection)
	router.GET("/webrtc/status", h.GetStatus)
}
