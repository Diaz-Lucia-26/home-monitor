package webrtc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"

	"home-monitor/internal/config"
)

// RTPForwarder RTP 转发器 - 从 JPEG 帧编码为 VP8/Opus RTP 流
type RTPForwarder struct {
	cameraID  string
	camConfig config.CameraConfig

	// FFmpeg 视频编码进程（JPEG -> VP8）
	videoCmd      *exec.Cmd
	videoCmdMutex sync.Mutex
	videoStdin    io.WriteCloser

	// FFmpeg 音频编码进程（PCM -> Opus）
	audioCmd      *exec.Cmd
	audioCmdMutex sync.Mutex
	audioStdin    io.WriteCloser

	// RTP 接收端口
	videoPort int
	audioPort int
	videoConn *net.UDPConn
	audioConn *net.UDPConn

	// WebRTC 轨道
	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP

	// JPEG 帧输入
	frameInput chan []byte

	// PCM 音频输入
	audioInput chan []byte

	// 状态
	running bool
	mutex   sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc

	// 订阅者计数
	subscribers int
	subMutex    sync.Mutex

	// 是否有音频
	hasAudio bool
}

// NewRTPForwarder 创建 RTP 转发器
func NewRTPForwarder(cameraID string, camConfig config.CameraConfig, videoPort, audioPort int) *RTPForwarder {
	return &RTPForwarder{
		cameraID:   cameraID,
		camConfig:  camConfig,
		videoPort:  videoPort,
		audioPort:  audioPort,
		frameInput: make(chan []byte, 10),
		audioInput: make(chan []byte, 100),
		hasAudio:   camConfig.Audio.Enabled,
	}
}

// Start 启动 RTP 转发器
func (f *RTPForwarder) Start(ctx context.Context) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.running {
		return nil
	}

	f.ctx, f.cancel = context.WithCancel(ctx)

	// 创建 WebRTC 轨道
	var err error

	// 视频轨道 - VP8
	f.videoTrack, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		fmt.Sprintf("video-%s", f.cameraID),
		fmt.Sprintf("stream-%s", f.cameraID),
	)
	if err != nil {
		return fmt.Errorf("创建视频轨道失败: %w", err)
	}

	// 音频轨道 - Opus（静音，因为无法从 JPEG 获取音频）
	f.audioTrack, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		fmt.Sprintf("audio-%s", f.cameraID),
		fmt.Sprintf("stream-%s", f.cameraID),
	)
	if err != nil {
		return fmt.Errorf("创建音频轨道失败: %w", err)
	}

	// 创建 UDP 监听
	videoAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", f.videoPort))
	if err != nil {
		return err
	}
	f.videoConn, err = net.ListenUDP("udp", videoAddr)
	if err != nil {
		return fmt.Errorf("监听视频端口失败: %w", err)
	}

	// 启动 FFmpeg 视频编码器（JPEG stdin -> VP8 RTP）
	if err := f.startVideoEncoder(); err != nil {
		f.videoConn.Close()
		return err
	}

	// 启动 RTP 接收协程
	go f.receiveVideoRTP()

	// 启动帧输入协程
	go f.feedFrames()

	// 如果有音频，启动音频编码
	if f.hasAudio {
		audioAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", f.audioPort))
		if err != nil {
			f.videoConn.Close()
			return err
		}
		f.audioConn, err = net.ListenUDP("udp", audioAddr)
		if err != nil {
			f.videoConn.Close()
			return fmt.Errorf("监听音频端口失败: %w", err)
		}

		if err := f.startAudioEncoder(); err != nil {
			f.videoConn.Close()
			f.audioConn.Close()
			return err
		}

		go f.receiveAudioRTP()
		go f.feedAudio()
	}

	f.running = true
	log.Printf("RTP 转发器已启动: %s (视频端口: %d, 音频端口: %d, 音频: %v)", f.cameraID, f.videoPort, f.audioPort, f.hasAudio)

	return nil
}

// startVideoEncoder 启动 FFmpeg 视频编码器（从 stdin 读取 JPEG，输出 VP8 RTP）
func (f *RTPForwarder) startVideoEncoder() error {
	f.videoCmdMutex.Lock()
	defer f.videoCmdMutex.Unlock()

	args := []string{
		// 输入: 使用 mjpeg 格式（连续 JPEG 流）
		"-f", "mjpeg",
		"-framerate", fmt.Sprintf("%d", f.camConfig.FPS),
		"-i", "pipe:0",

		// 输出: VP8 RTP
		"-c:v", "libvpx",
		"-b:v", "1M",
		"-keyint_min", "30",
		"-g", "30",
		"-deadline", "realtime",
		"-cpu-used", "8",
		"-an", // 无音频
		"-f", "rtp",
		fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", f.videoPort),
	}

	log.Printf("启动 FFmpeg VP8: ffmpeg %v", args)

	f.videoCmd = exec.CommandContext(f.ctx, "ffmpeg", args...)

	var err error
	f.videoStdin, err = f.videoCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建 stdin 管道失败: %w", err)
	}

	// 捕获 stderr 用于调试
	stderr, _ := f.videoCmd.StderrPipe()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("FFmpeg VP8 [%s]: %s", f.cameraID, scanner.Text())
		}
	}()

	if err := f.videoCmd.Start(); err != nil {
		return fmt.Errorf("启动 FFmpeg 视频编码器失败: %w", err)
	}

	// 监控进程退出
	go func() {
		err := f.videoCmd.Wait()
		log.Printf("FFmpeg VP8 编码器退出: %s (错误: %v)", f.cameraID, err)
	}()

	log.Printf("FFmpeg VP8 编码器已启动: %s (PID: %d)", f.cameraID, f.videoCmd.Process.Pid)
	return nil
}

// startAudioEncoder 启动 FFmpeg 音频编码器（从 stdin 读取 PCM，输出 Opus RTP）
func (f *RTPForwarder) startAudioEncoder() error {
	f.audioCmdMutex.Lock()
	defer f.audioCmdMutex.Unlock()

	args := []string{
		// 输入: PCM S16LE 48kHz mono
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "1",
		"-i", "pipe:0",

		// 输出: Opus RTP
		"-c:a", "libopus",
		"-b:a", "48k",
		"-application", "lowdelay",
		"-vn",
		"-f", "rtp",
		fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", f.audioPort),
	}

	log.Printf("启动 FFmpeg Opus: ffmpeg %v", args)

	f.audioCmd = exec.CommandContext(f.ctx, "ffmpeg", args...)

	var err error
	f.audioStdin, err = f.audioCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建音频 stdin 管道失败: %w", err)
	}

	// 捕获 stderr
	stderr, _ := f.audioCmd.StderrPipe()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("FFmpeg Opus [%s]: %s", f.cameraID, scanner.Text())
		}
	}()

	if err := f.audioCmd.Start(); err != nil {
		return fmt.Errorf("启动 FFmpeg 音频编码器失败: %w", err)
	}

	// 监控进程退出
	go func() {
		err := f.audioCmd.Wait()
		log.Printf("FFmpeg Opus 编码器退出: %s (错误: %v)", f.cameraID, err)
	}()

	log.Printf("FFmpeg Opus 编码器已启动: %s (PID: %d)", f.cameraID, f.audioCmd.Process.Pid)
	return nil
}

// feedFrames 将 JPEG 帧喂给 FFmpeg
func (f *RTPForwarder) feedFrames() {
	frameCount := 0
	for {
		select {
		case <-f.ctx.Done():
			return
		case frame, ok := <-f.frameInput:
			if !ok {
				return
			}
			if f.videoStdin != nil && len(frame) > 0 {
				n, err := f.videoStdin.Write(frame)
				if err != nil {
					log.Printf("写入帧到编码器失败: %v", err)
					continue
				}
				frameCount++
				if frameCount == 1 || frameCount%100 == 0 {
					log.Printf("已写入 %d 帧到 VP8 编码器 (当前帧大小: %d bytes, 写入: %d)", frameCount, len(frame), n)
				}
			}
		}
	}
}

// feedAudio 将 PCM 音频喂给 FFmpeg
func (f *RTPForwarder) feedAudio() {
	audioCount := 0
	for {
		select {
		case <-f.ctx.Done():
			return
		case audio, ok := <-f.audioInput:
			if !ok {
				return
			}
			if f.audioStdin != nil && len(audio) > 0 {
				_, err := f.audioStdin.Write(audio)
				if err != nil {
					log.Printf("写入音频到编码器失败: %v", err)
					continue
				}
				audioCount++
				if audioCount == 1 || audioCount%500 == 0 {
					log.Printf("已写入 %d 音频帧到 Opus 编码器", audioCount)
				}
			}
		}
	}
}

// WriteAudio 写入 PCM 音频
func (f *RTPForwarder) WriteAudio(audio []byte) {
	if !f.hasAudio {
		return
	}
	select {
	case f.audioInput <- audio:
	default:
		// 缓冲区满，丢弃
	}
}

// WriteFrame 写入 JPEG 帧
func (f *RTPForwarder) WriteFrame(frame []byte) {
	// 验证 JPEG 数据
	if len(frame) < 2 {
		log.Printf("WriteFrame: 帧太小 (%d bytes)", len(frame))
		return
	}

	// JPEG 应该以 FF D8 开头，以 FF D9 结尾
	isJPEG := frame[0] == 0xFF && frame[1] == 0xD8
	hasEnd := len(frame) >= 2 && frame[len(frame)-2] == 0xFF && frame[len(frame)-1] == 0xD9

	if !isJPEG {
		log.Printf("WriteFrame: 不是有效的 JPEG (开头: %02X %02X)", frame[0], frame[1])
		return
	}

	if !hasEnd {
		log.Printf("WriteFrame: JPEG 没有正确的结束标记 (结尾: %02X %02X)", frame[len(frame)-2], frame[len(frame)-1])
		// 仍然尝试发送
	}

	select {
	case f.frameInput <- frame:
	default:
		// 缓冲区满，丢弃
	}
}

// receiveVideoRTP 接收视频 RTP 包
func (f *RTPForwarder) receiveVideoRTP() {
	buf := make([]byte, 1500)
	packetCount := 0

	for {
		select {
		case <-f.ctx.Done():
			return
		default:
		}

		f.videoConn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := f.videoConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err != io.EOF {
				log.Printf("读取视频 RTP 失败: %v", err)
			}
			continue
		}

		// 解析 RTP 包
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buf[:n]); err != nil {
			continue
		}

		packetCount++
		if packetCount == 1 || packetCount%300 == 0 {
			log.Printf("视频 RTP 包统计: %d 个包已转发 (seq=%d, ts=%d)", packetCount, packet.SequenceNumber, packet.Timestamp)
		}

		// 写入到 WebRTC 轨道
		if err := f.videoTrack.WriteRTP(packet); err != nil {
			if err != io.ErrClosedPipe {
				log.Printf("写入视频轨道失败: %v", err)
			}
		}
	}
}

// receiveAudioRTP 接收音频 RTP 包
func (f *RTPForwarder) receiveAudioRTP() {
	buf := make([]byte, 1500)
	packetCount := 0

	for {
		select {
		case <-f.ctx.Done():
			return
		default:
		}

		f.audioConn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := f.audioConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err != io.EOF {
				log.Printf("读取音频 RTP 失败: %v", err)
			}
			continue
		}

		// 解析 RTP 包
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buf[:n]); err != nil {
			continue
		}

		packetCount++
		if packetCount == 1 || packetCount%500 == 0 {
			log.Printf("音频 RTP 包统计: %d 个包已转发 (seq=%d, ts=%d)", packetCount, packet.SequenceNumber, packet.Timestamp)
		}

		// 写入到 WebRTC 轨道
		if err := f.audioTrack.WriteRTP(packet); err != nil {
			if err != io.ErrClosedPipe {
				log.Printf("写入音频轨道失败: %v", err)
			}
		}
	}
}

// Stop 停止 RTP 转发器
func (f *RTPForwarder) Stop() {
	f.mutex.Lock()
	if !f.running {
		f.mutex.Unlock()
		return
	}
	f.running = false
	f.mutex.Unlock()

	// 先取消 context，让所有 goroutine 退出
	if f.cancel != nil {
		f.cancel()
	}

	// 关闭 UDP 连接（这会让 RTP 接收 goroutine 退出）
	if f.videoConn != nil {
		f.videoConn.Close()
	}
	if f.audioConn != nil {
		f.audioConn.Close()
	}

	// 关闭 stdin（这会让 FFmpeg 进程退出）
	if f.videoStdin != nil {
		f.videoStdin.Close()
	}
	if f.audioStdin != nil {
		f.audioStdin.Close()
	}

	// 杀死进程（不等待，因为已经有 goroutine 在等待）
	f.videoCmdMutex.Lock()
	if f.videoCmd != nil && f.videoCmd.Process != nil {
		f.videoCmd.Process.Kill()
	}
	f.videoCmdMutex.Unlock()

	f.audioCmdMutex.Lock()
	if f.audioCmd != nil && f.audioCmd.Process != nil {
		f.audioCmd.Process.Kill()
	}
	f.audioCmdMutex.Unlock()

	log.Printf("RTP 转发器已停止: %s", f.cameraID)
}

// GetVideoTrack 获取视频轨道
func (f *RTPForwarder) GetVideoTrack() *webrtc.TrackLocalStaticRTP {
	return f.videoTrack
}

// GetAudioTrack 获取音频轨道
func (f *RTPForwarder) GetAudioTrack() *webrtc.TrackLocalStaticRTP {
	return f.audioTrack
}

// AddSubscriber 增加订阅者
func (f *RTPForwarder) AddSubscriber() {
	f.subMutex.Lock()
	f.subscribers++
	f.subMutex.Unlock()
}

// RemoveSubscriber 移除订阅者
func (f *RTPForwarder) RemoveSubscriber() int {
	f.subMutex.Lock()
	defer f.subMutex.Unlock()
	f.subscribers--
	return f.subscribers
}

// GetSubscriberCount 获取订阅者数量
func (f *RTPForwarder) GetSubscriberCount() int {
	f.subMutex.Lock()
	defer f.subMutex.Unlock()
	return f.subscribers
}

// IsRunning 是否运行中
func (f *RTPForwarder) IsRunning() bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.running
}
