package monitor

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"
)

// Metrics 性能指标
type Metrics struct {
	// 系统信息
	Timestamp  time.Time `json:"timestamp"`
	Uptime     string    `json:"uptime"`
	UptimeSecs int64     `json:"uptime_secs"`

	// CPU
	NumCPU       int   `json:"num_cpu"`
	NumGoroutine int   `json:"num_goroutine"`
	CGoCalls     int64 `json:"cgo_calls"`

	// 内存 (字节)
	MemAlloc      uint64 `json:"mem_alloc"`       // 当前分配的内存
	MemTotalAlloc uint64 `json:"mem_total_alloc"` // 累计分配的内存
	MemSys        uint64 `json:"mem_sys"`         // 从系统获取的内存
	MemHeapAlloc  uint64 `json:"mem_heap_alloc"`  // 堆分配
	MemHeapSys    uint64 `json:"mem_heap_sys"`    // 堆系统内存
	MemHeapInuse  uint64 `json:"mem_heap_inuse"`  // 堆使用中
	MemStackInuse uint64 `json:"mem_stack_inuse"` // 栈使用中

	// 内存 (可读格式)
	MemAllocStr     string `json:"mem_alloc_str"`
	MemSysStr       string `json:"mem_sys_str"`
	MemHeapAllocStr string `json:"mem_heap_alloc_str"`

	// GC
	NumGC        uint32  `json:"num_gc"`         // GC 次数
	LastGC       string  `json:"last_gc"`        // 上次 GC 时间
	NextGC       uint64  `json:"next_gc"`        // 下次 GC 目标
	PauseTotalNs uint64  `json:"pause_total_ns"` // GC 暂停总时间
	GCCPUPercent float64 `json:"gc_cpu_percent"` // GC CPU 占用百分比

	// 进程
	PID int `json:"pid"`
}

// HistoryPoint 历史数据点
type HistoryPoint struct {
	Timestamp    time.Time `json:"timestamp"`
	MemAlloc     uint64    `json:"mem_alloc"`
	MemSys       uint64    `json:"mem_sys"`
	NumGoroutine int       `json:"num_goroutine"`
	NumGC        uint32    `json:"num_gc"`
}

// Alert 告警信息
type Alert struct {
	Time     time.Time `json:"time"`
	Type     string    `json:"type"`
	Message  string    `json:"message"`
	Value    string    `json:"value"`
	Resolved bool      `json:"resolved"`
}

// Monitor 性能监控器
type Monitor struct {
	startTime time.Time

	// 历史数据 (最近 1 小时，每 5 秒一个点 = 720 个点)
	history     []HistoryPoint
	historySize int

	// 告警
	alerts      []Alert
	alertsLimit int

	// 阈值配置
	memThreshold       uint64 // 内存告警阈值 (字节)
	goroutineThreshold int    // Goroutine 告警阈值

	// 上次告警状态（避免重复告警）
	lastMemAlert       bool
	lastGoroutineAlert bool

	mutex sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMonitor 创建监控器
func NewMonitor() *Monitor {
	return &Monitor{
		startTime:          time.Now(),
		history:            make([]HistoryPoint, 0, 720),
		historySize:        720, // 1 小时的数据 (5秒间隔)
		alerts:             make([]Alert, 0),
		alertsLimit:        100,
		memThreshold:       512 * 1024 * 1024, // 512MB
		goroutineThreshold: 1000,
	}
}

// SetThresholds 设置告警阈值
func (m *Monitor) SetThresholds(memMB int, goroutines int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if memMB > 0 {
		m.memThreshold = uint64(memMB) * 1024 * 1024
	}
	if goroutines > 0 {
		m.goroutineThreshold = goroutines
	}
}

// Start 启动监控
func (m *Monitor) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	go m.collectLoop()

	log.Printf("📊 性能监控已启动 (内存阈值: %s, Goroutine阈值: %d)",
		formatBytes(m.memThreshold), m.goroutineThreshold)
}

// Stop 停止监控
func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	log.Println("📊 性能监控已停止")
}

// collectLoop 采集循环
func (m *Monitor) collectLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 立即采集一次
	m.collect()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.collect()
		}
	}
}

// collect 采集一次数据
func (m *Monitor) collect() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	point := HistoryPoint{
		Timestamp:    time.Now(),
		MemAlloc:     memStats.Alloc,
		MemSys:       memStats.Sys,
		NumGoroutine: runtime.NumGoroutine(),
		NumGC:        memStats.NumGC,
	}

	m.mutex.Lock()

	// 添加到历史
	m.history = append(m.history, point)
	if len(m.history) > m.historySize {
		m.history = m.history[1:]
	}

	// 检查告警
	m.checkAlerts(point, memStats)

	m.mutex.Unlock()

	// 采集进程历史（包括 FFmpeg 子进程）
	m.CollectProcessHistory()
}

// checkAlerts 检查告警条件
func (m *Monitor) checkAlerts(point HistoryPoint, memStats runtime.MemStats) {
	// 内存告警
	if point.MemAlloc > m.memThreshold {
		if !m.lastMemAlert {
			m.addAlert("memory",
				fmt.Sprintf("内存使用超过阈值: %s > %s",
					formatBytes(point.MemAlloc), formatBytes(m.memThreshold)),
				formatBytes(point.MemAlloc))
			m.lastMemAlert = true
		}
	} else if m.lastMemAlert {
		m.addAlert("memory_resolved",
			fmt.Sprintf("内存使用恢复正常: %s", formatBytes(point.MemAlloc)),
			formatBytes(point.MemAlloc))
		m.lastMemAlert = false
	}

	// Goroutine 告警
	if point.NumGoroutine > m.goroutineThreshold {
		if !m.lastGoroutineAlert {
			m.addAlert("goroutine",
				fmt.Sprintf("Goroutine 数量超过阈值: %d > %d",
					point.NumGoroutine, m.goroutineThreshold),
				fmt.Sprintf("%d", point.NumGoroutine))
			m.lastGoroutineAlert = true
		}
	} else if m.lastGoroutineAlert {
		m.addAlert("goroutine_resolved",
			fmt.Sprintf("Goroutine 数量恢复正常: %d", point.NumGoroutine),
			fmt.Sprintf("%d", point.NumGoroutine))
		m.lastGoroutineAlert = false
	}
}

// addAlert 添加告警
func (m *Monitor) addAlert(alertType, message, value string) {
	alert := Alert{
		Time:     time.Now(),
		Type:     alertType,
		Message:  message,
		Value:    value,
		Resolved: alertType == "memory_resolved" || alertType == "goroutine_resolved",
	}

	m.alerts = append(m.alerts, alert)
	if len(m.alerts) > m.alertsLimit {
		m.alerts = m.alerts[1:]
	}

	// 输出日志
	if alert.Resolved {
		log.Printf("✅ [告警恢复] %s", message)
	} else {
		log.Printf("⚠️ [告警] %s", message)
	}
}

// GetMetrics 获取当前指标
func (m *Monitor) GetMetrics() Metrics {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(m.startTime)

	lastGCTime := ""
	if memStats.LastGC > 0 {
		lastGCTime = time.Unix(0, int64(memStats.LastGC)).Format("15:04:05")
	}

	return Metrics{
		Timestamp:  time.Now(),
		Uptime:     formatDuration(uptime),
		UptimeSecs: int64(uptime.Seconds()),

		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		CGoCalls:     runtime.NumCgoCall(),

		MemAlloc:      memStats.Alloc,
		MemTotalAlloc: memStats.TotalAlloc,
		MemSys:        memStats.Sys,
		MemHeapAlloc:  memStats.HeapAlloc,
		MemHeapSys:    memStats.HeapSys,
		MemHeapInuse:  memStats.HeapInuse,
		MemStackInuse: memStats.StackInuse,

		MemAllocStr:     formatBytes(memStats.Alloc),
		MemSysStr:       formatBytes(memStats.Sys),
		MemHeapAllocStr: formatBytes(memStats.HeapAlloc),

		NumGC:        memStats.NumGC,
		LastGC:       lastGCTime,
		NextGC:       memStats.NextGC,
		PauseTotalNs: memStats.PauseTotalNs,
		GCCPUPercent: memStats.GCCPUFraction * 100,

		PID: os.Getpid(),
	}
}

// GetHistory 获取历史数据
func (m *Monitor) GetHistory(minutes int) []HistoryPoint {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if minutes <= 0 {
		minutes = 60 // 默认 1 小时
	}

	// 计算需要的数据点数 (每 5 秒一个点)
	points := minutes * 12
	if points > len(m.history) {
		points = len(m.history)
	}

	if points == 0 {
		return []HistoryPoint{}
	}

	// 返回最近的 N 个点
	result := make([]HistoryPoint, points)
	copy(result, m.history[len(m.history)-points:])
	return result
}

// GetAlerts 获取告警列表
func (m *Monitor) GetAlerts(limit int) []Alert {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if limit <= 0 || limit > len(m.alerts) {
		limit = len(m.alerts)
	}

	// 返回最近的告警（倒序）
	result := make([]Alert, limit)
	for i := 0; i < limit; i++ {
		result[i] = m.alerts[len(m.alerts)-1-i]
	}
	return result
}

// ForceGC 强制执行 GC
func (m *Monitor) ForceGC() {
	before := m.GetMetrics()
	runtime.GC()
	after := m.GetMetrics()

	freed := int64(before.MemAlloc) - int64(after.MemAlloc)
	log.Printf("🗑️ 手动 GC 完成: 释放 %s (之前: %s, 之后: %s)",
		formatBytes(uint64(max(freed, 0))),
		before.MemAllocStr,
		after.MemAllocStr)
}

// formatBytes 格式化字节数
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration 格式化时长
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
