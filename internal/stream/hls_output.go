package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"home-monitor/internal/capture"
	"home-monitor/internal/config"
)

// HLSOutput HLS 输出推流器
// 将摄像头画面转换为 HLS 格式，可供外部访问
type HLSOutput struct {
	capturer     capture.AVCapturer
	camConfig    config.CameraConfig
	streamConfig config.StreamConfig
	outputPath   string

	cmd        *exec.Cmd
	videoStdin io.WriteCloser
	audioStdin io.WriteCloser

	running bool
	mutex   sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewHLSOutput 创建 HLS 输出
func NewHLSOutput(cap capture.AVCapturer, camCfg config.CameraConfig, streamCfg config.StreamConfig, outputPath string) *HLSOutput {
	return &HLSOutput{
		capturer:     cap,
		camConfig:    camCfg,
		streamConfig: streamCfg,
		outputPath:   outputPath,
	}
}

// Start 启动 HLS 输出
func (h *HLSOutput) Start(ctx context.Context) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if h.running {
		return nil
	}

	h.ctx, h.cancel = context.WithCancel(ctx)

	// 创建输出目录
	hlsDir := filepath.Join(h.outputPath, h.capturer.GetID())
	if err := os.MkdirAll(hlsDir, 0755); err != nil {
		return fmt.Errorf("创建 HLS 输出目录失败: %w", err)
	}

	// 播放列表路径
	playlistPath := filepath.Join(hlsDir, "index.m3u8")

	// 创建管道
	videoReader, videoWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("创建视频管道失败: %w", err)
	}

	audioReader, audioWriter, err := os.Pipe()
	if err != nil {
		videoReader.Close()
		videoWriter.Close()
		return fmt.Errorf("创建音频管道失败: %w", err)
	}

	h.videoStdin = videoWriter
	h.audioStdin = audioWriter

	// FFmpeg 参数：MJPEG + PCM -> H.264 + AAC -> HLS
	segmentDuration := h.streamConfig.HLSSegmentDuration
	if segmentDuration <= 0 {
		segmentDuration = 2
	}
	playlistLength := h.streamConfig.HLSPlaylistLength
	if playlistLength <= 0 {
		playlistLength = 5
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",

		// 视频输入 (MJPEG from pipe:3)
		"-f", "mjpeg",
		"-framerate", fmt.Sprintf("%d", h.camConfig.FPS),
		"-i", "pipe:3",

		// 音频输入 (PCM s16le from pipe:4)
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "1",
		"-i", "pipe:4",

		// 视频编码 (H.264)
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-profile:v", "baseline",
		"-level", "3.1",
		"-b:v", "1500k",
		"-maxrate", "2000k",
		"-bufsize", "3000k",
		"-g", fmt.Sprintf("%d", h.camConfig.FPS*2),
		"-sc_threshold", "0",
		"-pix_fmt", "yuv420p",

		// 音频编码 (AAC)
		"-c:a", "aac",
		"-b:a", "128k",
		"-ar", "44100",

		// HLS 输出
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", segmentDuration),
		"-hls_list_size", fmt.Sprintf("%d", playlistLength),
		"-hls_flags", "delete_segments+append_list",
		"-hls_segment_filename", filepath.Join(hlsDir, "segment_%03d.ts"),
		playlistPath,
	}

	log.Printf("启动 HLS 输出: %s -> %s", h.capturer.GetID(), playlistPath)

	h.cmd = exec.CommandContext(h.ctx, "ffmpeg", args...)
	h.cmd.ExtraFiles = []*os.File{videoReader, audioReader}

	// 捕获 stderr
	stderr, _ := h.cmd.StderrPipe()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("HLS [%s]: %s", h.capturer.GetID(), scanner.Text())
		}
	}()

	if err := h.cmd.Start(); err != nil {
		videoReader.Close()
		videoWriter.Close()
		audioReader.Close()
		audioWriter.Close()
		return fmt.Errorf("启动 HLS FFmpeg 失败: %w", err)
	}

	// 关闭读取端
	videoReader.Close()
	audioReader.Close()

	// 监控进程
	go func() {
		err := h.cmd.Wait()
		h.mutex.Lock()
		wasRunning := h.running
		h.running = false
		h.mutex.Unlock()
		if wasRunning && err != nil {
			log.Printf("HLS 输出进程退出: %s (错误: %v)", h.capturer.GetID(), err)
		}
	}()

	// 订阅视频帧
	go h.feedVideo()

	// 订阅音频
	if h.capturer.HasAudio() {
		go h.feedAudio()
	}

	h.running = true
	log.Printf("HLS 输出已启动: %s (播放地址: /hls/%s/index.m3u8)", h.capturer.GetID(), h.capturer.GetID())

	return nil
}

// feedVideo 发送视频帧
func (h *HLSOutput) feedVideo() {
	subID := fmt.Sprintf("hls_video_%s", h.capturer.GetID())
	frameCh := h.capturer.SubscribeFrames(subID)
	defer h.capturer.UnsubscribeFrames(subID)

	for {
		select {
		case <-h.ctx.Done():
			return
		case frame, ok := <-frameCh:
			if !ok || !h.IsRunning() {
				return
			}
			if h.videoStdin != nil && len(frame) > 0 {
				h.videoStdin.Write(frame)
			}
		}
	}
}

// feedAudio 发送音频
func (h *HLSOutput) feedAudio() {
	subID := fmt.Sprintf("hls_audio_%s", h.capturer.GetID())
	audioCh := h.capturer.SubscribeAudio(subID)
	defer h.capturer.UnsubscribeAudio(subID)

	for {
		select {
		case <-h.ctx.Done():
			return
		case audio, ok := <-audioCh:
			if !ok || !h.IsRunning() {
				return
			}
			if h.audioStdin != nil && len(audio) > 0 {
				h.audioStdin.Write(audio)
			}
		}
	}
}

// Stop 停止 HLS 输出
func (h *HLSOutput) Stop() {
	h.mutex.Lock()
	if !h.running {
		h.mutex.Unlock()
		return
	}
	h.running = false
	h.mutex.Unlock()

	if h.cancel != nil {
		h.cancel()
	}

	if h.videoStdin != nil {
		h.videoStdin.Close()
		h.videoStdin = nil
	}

	if h.audioStdin != nil {
		h.audioStdin.Close()
		h.audioStdin = nil
	}

	if h.cmd != nil && h.cmd.Process != nil {
		h.cmd.Process.Kill()
	}

	log.Printf("HLS 输出已停止: %s", h.capturer.GetID())
}

// IsRunning 是否运行中
func (h *HLSOutput) IsRunning() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.running
}

// GetPlaylistURL 获取播放列表相对 URL
func (h *HLSOutput) GetPlaylistURL() string {
	return fmt.Sprintf("/hls/%s/index.m3u8", h.capturer.GetID())
}

// HLSOutputManager HLS 输出管理器
type HLSOutputManager struct {
	outputs        map[string]*HLSOutput
	captureManager *capture.Manager
	cameras        map[string]config.CameraConfig
	streamConfig   config.StreamConfig
	outputPath     string
	mutex          sync.RWMutex
	ctx            context.Context
}

// NewHLSOutputManager 创建 HLS 输出管理器
func NewHLSOutputManager(ctx context.Context, capManager *capture.Manager, cameras []config.CameraConfig, streamCfg config.StreamConfig) *HLSOutputManager {
	m := &HLSOutputManager{
		outputs:        make(map[string]*HLSOutput),
		captureManager: capManager,
		cameras:        make(map[string]config.CameraConfig),
		streamConfig:   streamCfg,
		outputPath:     filepath.Join(streamCfg.TempPath, "hls"),
		ctx:            ctx,
	}

	for _, cam := range cameras {
		if cam.Enabled {
			m.cameras[cam.ID] = cam
		}
	}

	return m
}

// StartOutput 启动指定摄像头的 HLS 输出
func (m *HLSOutputManager) StartOutput(cameraID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if output, exists := m.outputs[cameraID]; exists && output.IsRunning() {
		return fmt.Errorf("HLS 输出已在运行: %s", cameraID)
	}

	camCfg, exists := m.cameras[cameraID]
	if !exists {
		return fmt.Errorf("摄像头不存在: %s", cameraID)
	}

	capturer, err := m.captureManager.GetCapturer(cameraID)
	if err != nil {
		return fmt.Errorf("获取采集器失败: %w", err)
	}

	output := NewHLSOutput(capturer, camCfg, m.streamConfig, m.outputPath)
	if err := output.Start(m.ctx); err != nil {
		return err
	}

	m.outputs[cameraID] = output
	return nil
}

// StopOutput 停止指定摄像头的 HLS 输出
func (m *HLSOutputManager) StopOutput(cameraID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if output, exists := m.outputs[cameraID]; exists {
		output.Stop()
		delete(m.outputs, cameraID)
	}
	return nil
}

// GetOutputStatus 获取 HLS 输出状态
func (m *HLSOutputManager) GetOutputStatus(cameraID string) (bool, string) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if output, exists := m.outputs[cameraID]; exists && output.IsRunning() {
		return true, output.GetPlaylistURL()
	}
	return false, ""
}

// GetAllOutputs 获取所有 HLS 输出状态
func (m *HLSOutputManager) GetAllOutputs() map[string]string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	outputs := make(map[string]string)
	for id, output := range m.outputs {
		if output.IsRunning() {
			outputs[id] = output.GetPlaylistURL()
		}
	}
	return outputs
}

// StopAll 停止所有输出
func (m *HLSOutputManager) StopAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, output := range m.outputs {
		output.Stop()
		delete(m.outputs, id)
	}
}

// GetOutputPath 获取 HLS 文件输出目录
func (m *HLSOutputManager) GetOutputPath() string {
	return m.outputPath
}
