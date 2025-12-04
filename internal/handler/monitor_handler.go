package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"home-monitor/internal/monitor"
)

// MonitorHandler 性能监控处理器
type MonitorHandler struct {
	monitor *monitor.Monitor
}

// NewMonitorHandler 创建监控处理器
func NewMonitorHandler(mon *monitor.Monitor) *MonitorHandler {
	return &MonitorHandler{
		monitor: mon,
	}
}

// GetMetrics 获取当前性能指标
func (h *MonitorHandler) GetMetrics(c *gin.Context) {
	metrics := h.monitor.GetMetrics()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    metrics,
	})
}

// GetHistory 获取历史数据
func (h *MonitorHandler) GetHistory(c *gin.Context) {
	minutes := 60
	if m := c.Query("minutes"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 {
			minutes = v
		}
	}

	history := h.monitor.GetHistory(minutes)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    history,
		"count":   len(history),
	})
}

// GetAlerts 获取告警列表
func (h *MonitorHandler) GetAlerts(c *gin.Context) {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	alerts := h.monitor.GetAlerts(limit)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    alerts,
		"count":   len(alerts),
	})
}

// ForceGC 强制执行 GC
func (h *MonitorHandler) ForceGC(c *gin.Context) {
	h.monitor.ForceGC()

	metrics := h.monitor.GetMetrics()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "GC 已执行",
		"data":    metrics,
	})
}

// GetSystemInfo 获取系统信息（包括子进程）
func (h *MonitorHandler) GetSystemInfo(c *gin.Context) {
	sysInfo := h.monitor.GetSystemInfo()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    sysInfo,
	})
}

// GetProcessHistory 获取进程历史数据
func (h *MonitorHandler) GetProcessHistory(c *gin.Context) {
	minutes := 60
	if m := c.Query("minutes"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 {
			minutes = v
		}
	}

	history := h.monitor.GetProcessHistory(minutes)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    history,
		"count":   len(history),
	})
}

// GetDiskUsage 获取磁盘使用情况
func (h *MonitorHandler) GetDiskUsage(c *gin.Context) {
	path := c.DefaultQuery("path", "./recordings")

	usage, err := monitor.GetDiskUsage(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    usage,
	})
}

// RegisterRoutes 注册监控路由
func (h *MonitorHandler) RegisterRoutes(group *gin.RouterGroup) {
	monitorGroup := group.Group("/monitor")
	{
		monitorGroup.GET("/metrics", h.GetMetrics)
		monitorGroup.GET("/history", h.GetHistory)
		monitorGroup.GET("/alerts", h.GetAlerts)
		monitorGroup.POST("/gc", h.ForceGC)
		monitorGroup.GET("/system", h.GetSystemInfo)
		monitorGroup.GET("/processes", h.GetProcessHistory)
		monitorGroup.GET("/disk", h.GetDiskUsage)
	}
}
