package rtmp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"

	"home-monitor/internal/config"
)

// Streamer RTMP 推流器（音视频合并版本）
type Streamer struct {
	cameraID  string
	camConfig config.CameraConfig
	rtmpURL   string

	cmd        *exec.Cmd
	videoStdin io.WriteCloser
	audioStdin io.WriteCloser

	frameInput chan []byte
	audioInput chan []byte

	running bool
	mutex   sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewStreamer 创建 RTMP 推流器
func NewStreamer(cameraID string, camConfig config.CameraConfig, rtmpURL string) *Streamer {
	return &Streamer{
		cameraID:   cameraID,
		camConfig:  camConfig,
		rtmpURL:    rtmpURL,
		frameInput: make(chan []byte, 30),
		audioInput: make(chan []byte, 100),
	}
}

// Start 启动 RTMP 推流（带音频）
func (s *Streamer) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return nil
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	// 创建视频和音频管道
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

	s.videoStdin = videoWriter
	s.audioStdin = audioWriter

	// 启动 FFmpeg 推流进程
	// 使用 pipe:3 和 pipe:4 作为视频和音频输入
	// 输出: H.264 + AAC -> RTMP/FLV
	args := []string{
		// 全局选项
		"-hide_banner",
		"-loglevel", "warning",

		// 视频输入 (MJPEG from pipe:3)
		"-f", "mjpeg",
		"-framerate", fmt.Sprintf("%d", s.camConfig.FPS),
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
		"-b:v", "2000k",
		"-maxrate", "2500k",
		"-bufsize", "4000k",
		"-g", fmt.Sprintf("%d", s.camConfig.FPS*2),
		"-keyint_min", fmt.Sprintf("%d", s.camConfig.FPS),
		"-sc_threshold", "0",
		"-pix_fmt", "yuv420p",

		// 音频编码 (AAC)
		"-c:a", "aac",
		"-b:a", "128k",
		"-ar", "44100",

		// 输出格式
		"-f", "flv",
		"-flvflags", "no_duration_filesize",
		s.rtmpURL,
	}

	log.Printf("启动 RTMP 推流: ffmpeg %v", args)

	s.cmd = exec.CommandContext(s.ctx, "ffmpeg", args...)

	// 传递额外的文件描述符
	// pipe:3 = videoReader, pipe:4 = audioReader
	s.cmd.ExtraFiles = []*os.File{videoReader, audioReader}

	// 捕获 stderr
	stderr, _ := s.cmd.StderrPipe()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("RTMP [%s]: %s", s.cameraID, scanner.Text())
		}
	}()

	if err := s.cmd.Start(); err != nil {
		videoReader.Close()
		videoWriter.Close()
		audioReader.Close()
		audioWriter.Close()
		return fmt.Errorf("启动 FFmpeg RTMP 推流失败: %w", err)
	}

	// 关闭读取端（由 FFmpeg 使用）
	videoReader.Close()
	audioReader.Close()

	// 监控进程退出
	go func() {
		err := s.cmd.Wait()
		s.mutex.Lock()
		wasRunning := s.running
		s.running = false
		s.mutex.Unlock()

		if wasRunning {
			log.Printf("RTMP 推流进程异常退出: %s (错误: %v)", s.cameraID, err)
		}
	}()

	// 启动视频帧发送协程
	go s.feedFrames()

	// 启动音频发送协程
	go s.feedAudio()

	s.running = true
	log.Printf("RTMP 推流已启动（含音频）: %s -> %s", s.cameraID, s.rtmpURL)

	return nil
}

// feedFrames 发送视频帧到 FFmpeg
func (s *Streamer) feedFrames() {
	frameCount := 0
	errCount := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case frame, ok := <-s.frameInput:
			if !ok {
				return
			}
			if !s.IsRunning() {
				return
			}
			if s.videoStdin != nil && len(frame) > 0 {
				_, err := s.videoStdin.Write(frame)
				if err != nil {
					errCount++
					if errCount <= 3 {
						log.Printf("RTMP 写入视频帧失败: %v", err)
					}
					if errCount == 3 {
						log.Printf("RTMP 后续视频写入错误将不再显示...")
					}
					continue
				}
				errCount = 0
				frameCount++
				if frameCount == 1 || frameCount%300 == 0 {
					log.Printf("RTMP 已推送 %d 视频帧: %s", frameCount, s.cameraID)
				}
			}
		}
	}
}

// feedAudio 发送音频数据到 FFmpeg
func (s *Streamer) feedAudio() {
	audioCount := 0
	errCount := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case audio, ok := <-s.audioInput:
			if !ok {
				return
			}
			if !s.IsRunning() {
				return
			}
			if s.audioStdin != nil && len(audio) > 0 {
				_, err := s.audioStdin.Write(audio)
				if err != nil {
					errCount++
					if errCount <= 3 {
						log.Printf("RTMP 写入音频失败: %v", err)
					}
					if errCount == 3 {
						log.Printf("RTMP 后续音频写入错误将不再显示...")
					}
					continue
				}
				errCount = 0
				audioCount++
				if audioCount == 1 || audioCount%1000 == 0 {
					log.Printf("RTMP 已推送 %d 音频块: %s", audioCount, s.cameraID)
				}
			}
		}
	}
}

// WriteFrame 写入视频帧
func (s *Streamer) WriteFrame(frame []byte) {
	if !s.IsRunning() {
		return
	}
	select {
	case s.frameInput <- frame:
	default:
		// 缓冲区满，丢弃
	}
}

// WriteAudio 写入音频数据
func (s *Streamer) WriteAudio(audio []byte) {
	if !s.IsRunning() {
		return
	}
	select {
	case s.audioInput <- audio:
	default:
		// 缓冲区满，丢弃
	}
}

// Stop 停止 RTMP 推流
func (s *Streamer) Stop() {
	s.mutex.Lock()
	if !s.running {
		s.mutex.Unlock()
		return
	}
	s.running = false
	s.mutex.Unlock()

	log.Printf("正在停止 RTMP 推流: %s", s.cameraID)

	// 先取消 context
	if s.cancel != nil {
		s.cancel()
	}

	// 关闭写入管道
	if s.videoStdin != nil {
		s.videoStdin.Close()
		s.videoStdin = nil
	}

	if s.audioStdin != nil {
		s.audioStdin.Close()
		s.audioStdin = nil
	}

	// 强制杀死进程
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}

	log.Printf("RTMP 推流已停止: %s", s.cameraID)
}

// IsRunning 是否运行中
func (s *Streamer) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.running
}

// GetURL 获取 RTMP URL
func (s *Streamer) GetURL() string {
	return s.rtmpURL
}

// GetCameraID 获取摄像头 ID
func (s *Streamer) GetCameraID() string {
	return s.cameraID
}
