package monitor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ProcessInfo 进程信息
type ProcessInfo struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	CPUPercent float64 `json:"cpu_percent"`
	MemoryRSS  uint64  `json:"memory_rss"` // 常驻内存 (字节)
	MemoryVSZ  uint64  `json:"memory_vsz"` // 虚拟内存 (字节)
	MemoryStr  string  `json:"memory_str"` // 可读格式
	State      string  `json:"state"`      // 进程状态
	StartTime  string  `json:"start_time"` // 启动时间
	RunTime    string  `json:"run_time"`   // 运行时长
}

// SystemInfo 系统整体信息
type SystemInfo struct {
	// 主进程
	MainProcess ProcessInfo `json:"main_process"`

	// 子进程列表 (FFmpeg 等)
	ChildProcesses []ProcessInfo `json:"child_processes"`

	// 汇总
	TotalProcesses int     `json:"total_processes"`
	TotalMemory    uint64  `json:"total_memory"`
	TotalMemoryStr string  `json:"total_memory_str"`
	TotalCPU       float64 `json:"total_cpu"`
}

// GetSystemInfo 获取系统信息（包括子进程）
func (m *Monitor) GetSystemInfo() SystemInfo {
	mainPID := os.Getpid()

	info := SystemInfo{
		MainProcess:    getProcessInfo(mainPID),
		ChildProcesses: getChildProcesses(mainPID),
	}

	// 计算汇总
	info.TotalProcesses = 1 + len(info.ChildProcesses)
	info.TotalMemory = info.MainProcess.MemoryRSS
	info.TotalCPU = info.MainProcess.CPUPercent

	for _, child := range info.ChildProcesses {
		info.TotalMemory += child.MemoryRSS
		info.TotalCPU += child.CPUPercent
	}

	info.TotalMemoryStr = formatBytes(info.TotalMemory)

	return info
}

// getProcessInfo 获取单个进程信息 (macOS/Linux)
func getProcessInfo(pid int) ProcessInfo {
	info := ProcessInfo{
		PID:   pid,
		State: "unknown",
	}

	// 使用 ps 命令获取进程信息
	// macOS: ps -o pid,comm,state,rss,vsz,etime,%cpu -p <pid>
	cmd := exec.Command("ps", "-o", "pid,comm,state,rss,vsz,etime,%cpu", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return info
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return info
	}

	// 解析第二行（第一行是标题）
	fields := strings.Fields(lines[1])
	if len(fields) >= 7 {
		info.Name = fields[1]
		info.State = parseState(fields[2])

		// RSS (KB -> Bytes)
		if rss, err := strconv.ParseUint(fields[3], 10, 64); err == nil {
			info.MemoryRSS = rss * 1024
		}

		// VSZ (KB -> Bytes)
		if vsz, err := strconv.ParseUint(fields[4], 10, 64); err == nil {
			info.MemoryVSZ = vsz * 1024
		}

		info.RunTime = fields[5]

		// CPU %
		if cpu, err := strconv.ParseFloat(fields[6], 64); err == nil {
			info.CPUPercent = cpu
		}
	}

	info.MemoryStr = formatBytes(info.MemoryRSS)

	// 获取完整命令行
	info.Command = getCommandLine(pid)

	return info
}

// getChildProcesses 获取所有子进程
func getChildProcesses(parentPID int) []ProcessInfo {
	var children []ProcessInfo

	// 方法1: 使用 pgrep 找子进程
	cmd := exec.Command("pgrep", "-P", strconv.Itoa(parentPID))
	output, err := cmd.Output()
	if err != nil {
		// 尝试方法2: 直接查找 ffmpeg 进程
		return findFFmpegProcesses(parentPID)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && pid > 0 {
			procInfo := getProcessInfo(pid)
			children = append(children, procInfo)

			// 递归查找孙进程
			grandChildren := getChildProcesses(pid)
			children = append(children, grandChildren...)
		}
	}

	return children
}

// findFFmpegProcesses 直接查找 ffmpeg 进程
func findFFmpegProcesses(parentPID int) []ProcessInfo {
	var processes []ProcessInfo

	// 使用 ps 找所有 ffmpeg 进程
	cmd := exec.Command("ps", "-eo", "pid,ppid,comm,state,rss,vsz,etime,%cpu")
	output, err := cmd.Output()
	if err != nil {
		return processes
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 { // 跳过标题
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		// 检查是否是 ffmpeg 且父进程是我们的主进程
		comm := fields[2]
		ppid, _ := strconv.Atoi(fields[1])

		if strings.Contains(strings.ToLower(comm), "ffmpeg") && ppid == parentPID {
			pid, _ := strconv.Atoi(fields[0])
			info := ProcessInfo{
				PID:   pid,
				Name:  comm,
				State: parseState(fields[3]),
			}

			if rss, err := strconv.ParseUint(fields[4], 10, 64); err == nil {
				info.MemoryRSS = rss * 1024
			}
			if vsz, err := strconv.ParseUint(fields[5], 10, 64); err == nil {
				info.MemoryVSZ = vsz * 1024
			}

			info.RunTime = fields[6]

			if cpu, err := strconv.ParseFloat(fields[7], 64); err == nil {
				info.CPUPercent = cpu
			}

			info.MemoryStr = formatBytes(info.MemoryRSS)
			info.Command = getCommandLine(pid)

			processes = append(processes, info)
		}
	}

	return processes
}

// getCommandLine 获取进程命令行
func getCommandLine(pid int) string {
	// macOS/Linux: /proc/<pid>/cmdline 或使用 ps
	procPath := fmt.Sprintf("/proc/%d/cmdline", pid)
	if data, err := os.ReadFile(procPath); err == nil {
		// cmdline 用 \0 分隔参数
		return strings.ReplaceAll(string(data), "\x00", " ")
	}

	// macOS fallback: 使用 ps
	cmd := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	cmdLine := strings.TrimSpace(string(output))
	// 截断过长的命令
	if len(cmdLine) > 200 {
		cmdLine = cmdLine[:200] + "..."
	}

	return cmdLine
}

// parseState 解析进程状态
func parseState(state string) string {
	if len(state) == 0 {
		return "unknown"
	}

	switch state[0] {
	case 'R':
		return "running"
	case 'S':
		return "sleeping"
	case 'D':
		return "disk_sleep"
	case 'Z':
		return "zombie"
	case 'T':
		return "stopped"
	case 'I':
		return "idle"
	case 'U':
		return "uninterruptible"
	default:
		return state
	}
}

// ProcessHistory 进程历史数据点
type ProcessHistory struct {
	Timestamp   time.Time `json:"timestamp"`
	MainMem     uint64    `json:"main_mem"`
	MainCPU     float64   `json:"main_cpu"`
	FFmpegMem   uint64    `json:"ffmpeg_mem"`
	FFmpegCPU   float64   `json:"ffmpeg_cpu"`
	TotalMem    uint64    `json:"total_mem"`
	TotalCPU    float64   `json:"total_cpu"`
	FFmpegCount int       `json:"ffmpeg_count"`
}

// processHistory 进程历史记录
var processHistory []ProcessHistory
var processHistorySize = 720 // 1小时

// CollectProcessHistory 采集进程历史（在 collectLoop 中调用）
func (m *Monitor) CollectProcessHistory() {
	sysInfo := m.GetSystemInfo()

	var ffmpegMem uint64
	var ffmpegCPU float64
	for _, child := range sysInfo.ChildProcesses {
		if strings.Contains(strings.ToLower(child.Name), "ffmpeg") {
			ffmpegMem += child.MemoryRSS
			ffmpegCPU += child.CPUPercent
		}
	}

	point := ProcessHistory{
		Timestamp:   time.Now(),
		MainMem:     sysInfo.MainProcess.MemoryRSS,
		MainCPU:     sysInfo.MainProcess.CPUPercent,
		FFmpegMem:   ffmpegMem,
		FFmpegCPU:   ffmpegCPU,
		TotalMem:    sysInfo.TotalMemory,
		TotalCPU:    sysInfo.TotalCPU,
		FFmpegCount: len(sysInfo.ChildProcesses),
	}

	m.mutex.Lock()
	processHistory = append(processHistory, point)
	if len(processHistory) > processHistorySize {
		processHistory = processHistory[1:]
	}
	m.mutex.Unlock()
}

// GetProcessHistory 获取进程历史
func (m *Monitor) GetProcessHistory(minutes int) []ProcessHistory {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if minutes <= 0 {
		minutes = 60
	}

	points := minutes * 12
	if points > len(processHistory) {
		points = len(processHistory)
	}

	if points == 0 {
		return []ProcessHistory{}
	}

	result := make([]ProcessHistory, points)
	copy(result, processHistory[len(processHistory)-points:])
	return result
}

// GetFFmpegStats 获取 FFmpeg 统计信息（从 stderr 解析）
type FFmpegStats struct {
	CameraID string  `json:"camera_id"`
	Frame    int64   `json:"frame"`
	FPS      float64 `json:"fps"`
	Bitrate  string  `json:"bitrate"`
	Speed    string  `json:"speed"`
	Time     string  `json:"time"`
	Size     string  `json:"size"`
}

// ParseFFmpegProgress 解析 FFmpeg 进度输出
func ParseFFmpegProgress(line string) *FFmpegStats {
	// FFmpeg 输出格式:
	// frame=  123 fps= 30 q=5.0 size=   1234KiB time=00:00:04.10 bitrate=2468.5kbits/s speed=1.0x
	stats := &FFmpegStats{}

	// 解析 frame
	if idx := strings.Index(line, "frame="); idx >= 0 {
		part := line[idx+6:]
		fields := strings.Fields(part)
		if len(fields) > 0 {
			stats.Frame, _ = strconv.ParseInt(fields[0], 10, 64)
		}
	}

	// 解析 fps
	if idx := strings.Index(line, "fps="); idx >= 0 {
		part := line[idx+4:]
		fields := strings.Fields(part)
		if len(fields) > 0 {
			stats.FPS, _ = strconv.ParseFloat(fields[0], 64)
		}
	}

	// 解析 size
	if idx := strings.Index(line, "size="); idx >= 0 {
		part := line[idx+5:]
		fields := strings.Fields(part)
		if len(fields) > 0 {
			stats.Size = fields[0]
		}
	}

	// 解析 time
	if idx := strings.Index(line, "time="); idx >= 0 {
		part := line[idx+5:]
		fields := strings.Fields(part)
		if len(fields) > 0 {
			stats.Time = fields[0]
		}
	}

	// 解析 bitrate
	if idx := strings.Index(line, "bitrate="); idx >= 0 {
		part := line[idx+8:]
		fields := strings.Fields(part)
		if len(fields) > 0 {
			stats.Bitrate = fields[0]
		}
	}

	// 解析 speed
	if idx := strings.Index(line, "speed="); idx >= 0 {
		part := line[idx+6:]
		fields := strings.Fields(part)
		if len(fields) > 0 {
			stats.Speed = fields[0]
		}
	}

	return stats
}

// ReadProcStat 读取 /proc/stat 获取 CPU 使用率 (Linux only)
func ReadProcStat() (user, system, idle uint64, err error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				user, _ = strconv.ParseUint(fields[1], 10, 64)
				system, _ = strconv.ParseUint(fields[3], 10, 64)
				idle, _ = strconv.ParseUint(fields[4], 10, 64)
				return user, system, idle, nil
			}
		}
	}

	return 0, 0, 0, fmt.Errorf("cpu line not found")
}

// GetDiskUsage 获取磁盘使用情况
type DiskUsage struct {
	Path     string  `json:"path"`
	Total    uint64  `json:"total"`
	Used     uint64  `json:"used"`
	Free     uint64  `json:"free"`
	UsedPct  float64 `json:"used_percent"`
	TotalStr string  `json:"total_str"`
	UsedStr  string  `json:"used_str"`
	FreeStr  string  `json:"free_str"`
}

// GetDiskUsage 获取指定路径的磁盘使用情况
func GetDiskUsage(path string) (*DiskUsage, error) {
	// 使用 df 命令
	absPath, _ := filepath.Abs(path)
	cmd := exec.Command("df", "-k", absPath)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected df output")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return nil, fmt.Errorf("unexpected df output format")
	}

	usage := &DiskUsage{Path: absPath}

	// df -k 输出单位是 KB
	if total, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
		usage.Total = total * 1024
	}
	if used, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
		usage.Used = used * 1024
	}
	if free, err := strconv.ParseUint(fields[3], 10, 64); err == nil {
		usage.Free = free * 1024
	}

	if usage.Total > 0 {
		usage.UsedPct = float64(usage.Used) / float64(usage.Total) * 100
	}

	usage.TotalStr = formatBytes(usage.Total)
	usage.UsedStr = formatBytes(usage.Used)
	usage.FreeStr = formatBytes(usage.Free)

	return usage, nil
}
