package capture

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"home-monitor/internal/config"
)

// AVCapturer 统一音视频采集器接口
type AVCapturer interface {
	Start(ctx context.Context) error
	Stop() error
	GetID() string
	GetName() string
	GetConfig() config.CameraConfig
	IsRunning() bool
	HasAudio() bool
	GetFrame() ([]byte, error)
	SubscribeFrames(id string) <-chan []byte
	UnsubscribeFrames(id string)
	SubscribeAudio(id string) <-chan []byte
	UnsubscribeAudio(id string)
}

// RecordingConfig 录制配置
type RecordingConfig struct {
	OutputPath      string
	SegmentDuration int // 秒
	Format          string
}

// FFmpegCapturer 基于 FFmpeg 的统一音视频采集器
// 使用单一 FFmpeg 进程同时输出：
// 1. MJPEG 帧流（用于 Web 预览）
// 2. 分段视频文件（用于录像存储）
type FFmpegCapturer struct {
	config config.CameraConfig

	// 主采集进程
	cmd      *exec.Cmd
	cmdMutex sync.Mutex

	// MJPEG 预览管道
	mjpegPipe io.ReadCloser

	// 音频管道 (PCM S16LE 48kHz mono)
	audioPipe io.ReadCloser

	// 音频订阅者
	audioSubscribers map[string]chan []byte
	audioMutex       sync.RWMutex

	running bool
	mutex   sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	// 帧缓存
	lastFrame   []byte
	lastFrameMu sync.RWMutex

	// 帧订阅者
	frameSubscribers map[string]chan []byte
	frameMutex       sync.RWMutex

	// 录制配置
	recordingConfig *RecordingConfig

	done chan struct{}
}

// NewAVCapturer 创建新的音视频采集器
func NewAVCapturer(cfg config.CameraConfig) AVCapturer {
	return &FFmpegCapturer{
		config:           cfg,
		frameSubscribers: make(map[string]chan []byte),
		audioSubscribers: make(map[string]chan []byte),
		done:             make(chan struct{}),
	}
}

// GetID 获取采集器ID
func (c *FFmpegCapturer) GetID() string {
	return c.config.ID
}

// GetName 获取采集器名称
func (c *FFmpegCapturer) GetName() string {
	return c.config.Name
}

// GetConfig 获取配置
func (c *FFmpegCapturer) GetConfig() config.CameraConfig {
	return c.config
}

// IsRunning 检查是否运行中
func (c *FFmpegCapturer) IsRunning() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.running
}

// HasAudio 是否启用了音频
func (c *FFmpegCapturer) HasAudio() bool {
	return c.config.Audio.Enabled
}

// SetRecordingConfig 设置录制配置
func (c *FFmpegCapturer) SetRecordingConfig(cfg RecordingConfig) {
	c.recordingConfig = &cfg
}

// Start 启动采集器
func (c *FFmpegCapturer) Start(ctx context.Context) error {
	c.mutex.Lock()
	if c.running {
		c.mutex.Unlock()
		return nil
	}
	c.mutex.Unlock()

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})

	// 启动 FFmpeg 进程
	if err := c.startCapture(); err != nil {
		return fmt.Errorf("启动采集失败: %w", err)
	}

	c.mutex.Lock()
	c.running = true
	c.mutex.Unlock()

	log.Printf("音视频采集器 %s (%s) 已启动", c.config.Name, c.config.ID)
	return nil
}

// Stop 停止采集器
func (c *FFmpegCapturer) Stop() error {
	c.mutex.Lock()
	if !c.running {
		c.mutex.Unlock()
		return nil
	}
	c.mutex.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	c.stopCapture()

	// 等待 goroutine 退出
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
	}

	// 关闭所有订阅者通道
	c.frameMutex.Lock()
	for id, ch := range c.frameSubscribers {
		close(ch)
		delete(c.frameSubscribers, id)
	}
	c.frameMutex.Unlock()

	c.audioMutex.Lock()
	for id, ch := range c.audioSubscribers {
		close(ch)
		delete(c.audioSubscribers, id)
	}
	c.audioMutex.Unlock()

	c.mutex.Lock()
	c.running = false
	c.mutex.Unlock()

	log.Printf("音视频采集器 %s (%s) 已停止", c.config.Name, c.config.ID)
	return nil
}

// startCapture 启动采集
func (c *FFmpegCapturer) startCapture() error {
	// 创建 MJPEG 管道
	mjpegPipeR, mjpegPipeW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("创建 MJPEG 管道失败: %w", err)
	}
	c.mjpegPipe = mjpegPipeR

	// 创建音频管道（如果启用音频）
	var audioPipeR, audioPipeW *os.File
	if c.config.Audio.Enabled {
		audioPipeR, audioPipeW, err = os.Pipe()
		if err != nil {
			mjpegPipeR.Close()
			mjpegPipeW.Close()
			return fmt.Errorf("创建音频管道失败: %w", err)
		}
		c.audioPipe = audioPipeR
	}

	// 构建 FFmpeg 参数
	args := c.buildCaptureArgs(mjpegPipeW, audioPipeW)

	c.cmdMutex.Lock()
	c.cmd = exec.CommandContext(c.ctx, "ffmpeg", args...)
	if c.config.Audio.Enabled {
		c.cmd.ExtraFiles = []*os.File{mjpegPipeW, audioPipeW} // fd 3, fd 4
	} else {
		c.cmd.ExtraFiles = []*os.File{mjpegPipeW} // fd 3
	}
	c.cmd.Stderr = os.Stderr // 调试输出
	c.cmdMutex.Unlock()

	if err := c.cmd.Start(); err != nil {
		mjpegPipeR.Close()
		mjpegPipeW.Close()
		if audioPipeR != nil {
			audioPipeR.Close()
			audioPipeW.Close()
		}
		return fmt.Errorf("启动 FFmpeg 失败: %w", err)
	}

	// 关闭写端（FFmpeg 进程已持有）
	mjpegPipeW.Close()
	if audioPipeW != nil {
		audioPipeW.Close()
	}

	// 启动 MJPEG 帧读取 goroutine
	go c.readMJPEGStream()

	// 启动音频读取 goroutine
	if c.config.Audio.Enabled {
		go c.readAudioStream()
	}

	// 监控进程退出
	go func() {
		c.cmd.Wait()
		c.cmdMutex.Lock()
		c.cmd = nil
		c.cmdMutex.Unlock()
	}()

	return nil
}

// stopCapture 停止采集
func (c *FFmpegCapturer) stopCapture() {
	c.cmdMutex.Lock()
	defer c.cmdMutex.Unlock()

	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Signal(os.Interrupt)
		time.Sleep(1 * time.Second)
		c.cmd.Process.Kill()
		c.cmd.Wait()
		c.cmd = nil
	}

	if c.mjpegPipe != nil {
		c.mjpegPipe.Close()
		c.mjpegPipe = nil
	}

	if c.audioPipe != nil {
		c.audioPipe.Close()
		c.audioPipe = nil
	}
}

// buildCaptureArgs 构建 FFmpeg 参数
func (c *FFmpegCapturer) buildCaptureArgs(mjpegPipeW *os.File, audioPipeW *os.File) []string {
	var args []string

	// 输入配置
	switch c.config.Type {
	case "rtsp":
		args = append(args,
			"-rtsp_transport", "tcp",
			"-i", c.config.RTSPUrl,
		)
	case "hls":
		// HLS/m3u8 流输入
		args = append(args,
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
			"-i", c.config.HLSUrl,
		)
	default:
		args = append(args, c.getInputArgs()...)
	}

	// 输出 1: MJPEG 预览流 -> pipe:3
	args = append(args,
		"-map", "0:v",
		"-an",
		"-f", "mjpeg",
		"-q:v", "5",
		"-r", fmt.Sprintf("%d", c.config.FPS),
		"-s", fmt.Sprintf("%dx%d", c.config.Width, c.config.Height),
		"pipe:3",
	)

	// 输出 2: 音频流 -> pipe:4 (PCM S16LE 48kHz mono，用于 WebRTC)
	if c.config.Audio.Enabled && audioPipeW != nil {
		args = append(args,
			"-map", "0:a",
			"-vn",
			"-f", "s16le",
			"-acodec", "pcm_s16le",
			"-ar", "48000",
			"-ac", "1",
			"pipe:4",
		)
	}

	// 输出 3: 分段录像文件（如果配置了录制）
	if c.recordingConfig != nil {
		// 确保目录存在
		outputDir := filepath.Join(c.recordingConfig.OutputPath, c.config.ID)
		os.MkdirAll(outputDir, 0755)

		// 文件名模板
		outputPattern := filepath.Join(outputDir, c.config.ID+"_%Y%m%d_%H%M%S."+c.recordingConfig.Format)

		if c.config.Audio.Enabled {
			// 有音频的录制
			args = append(args,
				"-map", "0:v",
				"-map", "0:a",
				"-c:v", "libx264",
				"-pix_fmt", "yuv420p",
				"-preset", "ultrafast",
				"-crf", "23",
				"-g", "60", // 关键帧间隔 2 秒（30fps * 2）
				"-c:a", "aac",
				"-b:a", "128k",
				"-f", "segment",
				"-segment_time", fmt.Sprintf("%d", c.recordingConfig.SegmentDuration),
				"-segment_format", c.recordingConfig.Format,
				// Fragmented MP4: 每个关键帧写入一个片段，异常中断也能保留已录制内容
				"-segment_format_options", "movflags=frag_keyframe+empty_moov+default_base_moof",
				"-reset_timestamps", "1",
				"-strftime", "1",
				outputPattern,
			)
		} else {
			// 无音频的录制
			args = append(args,
				"-map", "0:v",
				"-an",
				"-c:v", "libx264",
				"-pix_fmt", "yuv420p",
				"-preset", "ultrafast",
				"-crf", "23",
				"-g", "60", // 关键帧间隔 2 秒
				"-f", "segment",
				"-segment_time", fmt.Sprintf("%d", c.recordingConfig.SegmentDuration),
				"-segment_format", c.recordingConfig.Format,
				"-segment_format_options", "movflags=frag_keyframe+empty_moov+default_base_moof",
				"-reset_timestamps", "1",
				"-strftime", "1",
				outputPattern,
			)
		}
	}

	return args
}

// getInputArgs 获取输入参数
func (c *FFmpegCapturer) getInputArgs() []string {
	switch runtime.GOOS {
	case "darwin":
		if c.config.Audio.Enabled {
			deviceInput := fmt.Sprintf("%d:%d", c.config.DeviceIndex, c.config.Audio.DeviceIndex)
			return []string{
				"-f", "avfoundation",
				"-framerate", fmt.Sprintf("%d", c.config.FPS),
				"-video_size", fmt.Sprintf("%dx%d", c.config.Width, c.config.Height),
				"-i", deviceInput,
			}
		}
		return []string{
			"-f", "avfoundation",
			"-framerate", fmt.Sprintf("%d", c.config.FPS),
			"-video_size", fmt.Sprintf("%dx%d", c.config.Width, c.config.Height),
			"-i", fmt.Sprintf("%d:none", c.config.DeviceIndex),
		}

	case "linux":
		args := []string{
			"-f", "v4l2",
			"-framerate", fmt.Sprintf("%d", c.config.FPS),
			"-video_size", fmt.Sprintf("%dx%d", c.config.Width, c.config.Height),
			"-i", fmt.Sprintf("/dev/video%d", c.config.DeviceIndex),
		}
		if c.config.Audio.Enabled {
			if c.config.Audio.Type == "pulse" {
				args = append(args, "-f", "pulse", "-i", "default")
			} else {
				args = append(args, "-f", "alsa", "-i", fmt.Sprintf("hw:%d", c.config.Audio.DeviceIndex))
			}
		}
		return args

	case "windows":
		if c.config.Audio.Enabled {
			videoDevice := fmt.Sprintf("video=@device_pnp_%d", c.config.DeviceIndex)
			audioDevice := "Microphone"
			if c.config.Audio.DeviceName != "" {
				audioDevice = c.config.Audio.DeviceName
			}
			return []string{
				"-f", "dshow",
				"-i", fmt.Sprintf("%s:audio=%s", videoDevice, audioDevice),
			}
		}
		return []string{
			"-f", "dshow",
			"-i", fmt.Sprintf("video=@device_pnp_%d", c.config.DeviceIndex),
		}

	default:
		return []string{
			"-f", "v4l2",
			"-i", fmt.Sprintf("/dev/video%d", c.config.DeviceIndex),
		}
	}
}

// readMJPEGStream 读取 MJPEG 预览流
func (c *FFmpegCapturer) readMJPEGStream() {
	defer func() {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}()

	reader := bufio.NewReaderSize(c.mjpegPipe, 1024*1024)
	jpegStart := []byte{0xFF, 0xD8}
	jpegEnd := []byte{0xFF, 0xD9}
	var frameBuffer []byte

	buffer := make([]byte, 64*1024)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			n, err := reader.Read(buffer)
			if err != nil {
				if err != io.EOF && c.ctx.Err() == nil {
					log.Printf("读取 MJPEG 流错误: %v", err)
				}
				return
			}

			frameBuffer = append(frameBuffer, buffer[:n]...)

			// 解析 JPEG 帧
			for {
				startIdx := findBytes(frameBuffer, jpegStart)
				if startIdx == -1 {
					break
				}

				endIdx := findBytes(frameBuffer[startIdx:], jpegEnd)
				if endIdx == -1 {
					break
				}

				endIdx += startIdx + 2

				frame := make([]byte, endIdx-startIdx)
				copy(frame, frameBuffer[startIdx:endIdx])

				c.lastFrameMu.Lock()
				c.lastFrame = frame
				c.lastFrameMu.Unlock()

				c.broadcastFrame(frame)

				frameBuffer = frameBuffer[endIdx:]
			}

			// 防止缓冲区过大
			if len(frameBuffer) > 2*1024*1024 {
				frameBuffer = frameBuffer[len(frameBuffer)-1024*1024:]
			}
		}
	}
}

// broadcastFrame 广播帧数据给订阅者
func (c *FFmpegCapturer) broadcastFrame(frame []byte) {
	c.frameMutex.RLock()
	defer c.frameMutex.RUnlock()

	for _, ch := range c.frameSubscribers {
		frameCopy := make([]byte, len(frame))
		copy(frameCopy, frame)

		select {
		case ch <- frameCopy:
		default:
			// 缓冲区满，丢弃旧帧
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- frameCopy:
			default:
			}
		}
	}
}

// GetFrame 获取当前帧
func (c *FFmpegCapturer) GetFrame() ([]byte, error) {
	c.mutex.RLock()
	running := c.running
	c.mutex.RUnlock()

	if !running {
		return nil, fmt.Errorf("采集器未运行")
	}

	c.lastFrameMu.RLock()
	frame := c.lastFrame
	c.lastFrameMu.RUnlock()

	if frame != nil {
		result := make([]byte, len(frame))
		copy(result, frame)
		return result, nil
	}

	subID := fmt.Sprintf("snapshot_%d", time.Now().UnixNano())
	ch := c.SubscribeFrames(subID)
	defer c.UnsubscribeFrames(subID)

	select {
	case frame := <-ch:
		return frame, nil
	case <-time.After(3 * time.Second):
		return nil, fmt.Errorf("获取帧超时")
	}
}

// SubscribeFrames 订阅帧数据
func (c *FFmpegCapturer) SubscribeFrames(id string) <-chan []byte {
	c.frameMutex.Lock()
	defer c.frameMutex.Unlock()

	ch := make(chan []byte, 30)
	c.frameSubscribers[id] = ch
	return ch
}

// UnsubscribeFrames 取消订阅帧数据
func (c *FFmpegCapturer) UnsubscribeFrames(id string) {
	c.frameMutex.Lock()
	defer c.frameMutex.Unlock()

	if ch, exists := c.frameSubscribers[id]; exists {
		close(ch)
		delete(c.frameSubscribers, id)
	}
}

// readAudioStream 读取音频流
func (c *FFmpegCapturer) readAudioStream() {
	if c.audioPipe == nil {
		return
	}

	// 960 samples * 2 bytes * 1 channel = 1920 bytes = 20ms of audio at 48kHz
	// Opus 通常使用 20ms 帧
	const audioFrameSize = 960 * 2 * 1 // 1920 bytes per 20ms frame
	buffer := make([]byte, audioFrameSize)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			n, err := io.ReadFull(c.audioPipe, buffer)
			if err != nil {
				if err != io.EOF && c.ctx.Err() == nil {
					log.Printf("读取音频流错误: %v", err)
				}
				return
			}

			if n == audioFrameSize {
				c.broadcastAudio(buffer[:n])
			}
		}
	}
}

// broadcastAudio 广播音频数据给订阅者
func (c *FFmpegCapturer) broadcastAudio(audio []byte) {
	c.audioMutex.RLock()
	defer c.audioMutex.RUnlock()

	for _, ch := range c.audioSubscribers {
		audioCopy := make([]byte, len(audio))
		copy(audioCopy, audio)

		select {
		case ch <- audioCopy:
		default:
			// 缓冲区满，丢弃
		}
	}
}

// SubscribeAudio 订阅音频数据
func (c *FFmpegCapturer) SubscribeAudio(id string) <-chan []byte {
	c.audioMutex.Lock()
	defer c.audioMutex.Unlock()

	ch := make(chan []byte, 100) // 缓冲 100 个 20ms 帧 = 2秒
	c.audioSubscribers[id] = ch
	return ch
}

// UnsubscribeAudio 取消订阅音频数据
func (c *FFmpegCapturer) UnsubscribeAudio(id string) {
	c.audioMutex.Lock()
	defer c.audioMutex.Unlock()

	if ch, exists := c.audioSubscribers[id]; exists {
		close(ch)
		delete(c.audioSubscribers, id)
	}
}

// findBytes 查找字节序列
func findBytes(data, pattern []byte) int {
	if len(pattern) == 0 || len(data) < len(pattern) {
		return -1
	}
	for i := 0; i <= len(data)-len(pattern); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if data[i+j] != pattern[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Manager 采集器管理器
type Manager struct {
	capturers map[string]AVCapturer
	mutex     sync.RWMutex
}

// NewManager 创建采集器管理器
func NewManager() *Manager {
	return &Manager{
		capturers: make(map[string]AVCapturer),
	}
}

// AddCapturer 添加采集器
func (m *Manager) AddCapturer(cfg config.CameraConfig) (AVCapturer, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.capturers[cfg.ID]; exists {
		return nil, fmt.Errorf("采集器 %s 已存在", cfg.ID)
	}

	capturer := NewAVCapturer(cfg)
	m.capturers[cfg.ID] = capturer
	log.Printf("已添加采集器: %s (%s)", cfg.Name, cfg.ID)
	return capturer, nil
}

// AddCapturerWithRecording 添加带录制配置的采集器
func (m *Manager) AddCapturerWithRecording(cfg config.CameraConfig, recCfg RecordingConfig) (AVCapturer, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.capturers[cfg.ID]; exists {
		return nil, fmt.Errorf("采集器 %s 已存在", cfg.ID)
	}

	capturer := &FFmpegCapturer{
		config:           cfg,
		frameSubscribers: make(map[string]chan []byte),
		audioSubscribers: make(map[string]chan []byte),
		done:             make(chan struct{}),
		recordingConfig:  &recCfg,
	}
	m.capturers[cfg.ID] = capturer
	log.Printf("已添加采集器（带录制）: %s (%s)", cfg.Name, cfg.ID)
	return capturer, nil
}

// GetCapturer 获取采集器
func (m *Manager) GetCapturer(id string) (AVCapturer, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	capturer, exists := m.capturers[id]
	if !exists {
		return nil, fmt.Errorf("采集器 %s 不存在", id)
	}
	return capturer, nil
}

// GetAllCapturers 获取所有采集器
func (m *Manager) GetAllCapturers() []AVCapturer {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	capturers := make([]AVCapturer, 0, len(m.capturers))
	for _, c := range m.capturers {
		capturers = append(capturers, c)
	}
	return capturers
}

// StartAll 启动所有采集器
func (m *Manager) StartAll(ctx context.Context) error {
	capturers := m.GetAllCapturers()
	for _, c := range capturers {
		if err := c.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StopAll 停止所有采集器
func (m *Manager) StopAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, c := range m.capturers {
		c.Stop()
	}
}
