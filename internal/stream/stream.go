package stream

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"home-monitor/internal/capture"
	"home-monitor/internal/config"
)

// HLSStreamer HLS 流处理器
// 负责 MJPEG 帧分发（用于 Web 预览）
type HLSStreamer struct {
	capturer   capture.AVCapturer
	config     config.StreamConfig
	outputPath string
	running    bool
	mutex      sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}

	// MJPEG 订阅者（用于 Web 预览）
	subscribers     map[string]chan []byte
	subscriberMutex sync.RWMutex
}

// NewHLSStreamer 创建 HLS 流处理器
func NewHLSStreamer(cap capture.AVCapturer, streamCfg config.StreamConfig) *HLSStreamer {
	return &HLSStreamer{
		capturer:    cap,
		config:      streamCfg,
		subscribers: make(map[string]chan []byte),
		done:        make(chan struct{}),
	}
}

// Start 启动流处理器
func (s *HLSStreamer) Start(ctx context.Context) error {
	s.mutex.Lock()
	if s.running {
		s.mutex.Unlock()
		return nil
	}
	s.mutex.Unlock()

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.done = make(chan struct{})

	// 创建 HLS 输出目录（即使不用 HLS 也保留目录结构）
	s.outputPath = filepath.Join(s.config.TempPath, "hls", s.capturer.GetID())
	if err := os.MkdirAll(s.outputPath, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	s.mutex.Lock()
	s.running = true
	s.mutex.Unlock()

	// 启动 MJPEG 帧分发（用于 Web 预览）
	go s.distributeFrames()

	log.Printf("HLS 流 %s 已启动", s.capturer.GetID())
	return nil
}

// Stop 停止流处理器
func (s *HLSStreamer) Stop() error {
	s.mutex.Lock()
	if !s.running {
		s.mutex.Unlock()
		return nil
	}
	s.mutex.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}

	// 关闭订阅者
	s.subscriberMutex.Lock()
	for id, ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, id)
	}
	s.subscriberMutex.Unlock()

	s.mutex.Lock()
	s.running = false
	s.mutex.Unlock()

	log.Printf("HLS 流 %s 已停止", s.capturer.GetID())
	return nil
}

// Subscribe 订阅 MJPEG 视频流（用于 Web 预览）
func (s *HLSStreamer) Subscribe(id string) <-chan []byte {
	s.subscriberMutex.Lock()
	defer s.subscriberMutex.Unlock()

	ch := make(chan []byte, 10)
	s.subscribers[id] = ch
	return ch
}

// Unsubscribe 取消订阅
func (s *HLSStreamer) Unsubscribe(id string) {
	s.subscriberMutex.Lock()
	defer s.subscriberMutex.Unlock()

	if ch, exists := s.subscribers[id]; exists {
		close(ch)
		delete(s.subscribers, id)
	}
}

// GetPlaylistPath 获取 HLS 播放列表路径
func (s *HLSStreamer) GetPlaylistPath() string {
	return filepath.Join(s.outputPath, "playlist.m3u8")
}

// IsRunning 检查是否运行中
func (s *HLSStreamer) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.running
}

// distributeFrames 分发 MJPEG 帧到 Web 预览订阅者
func (s *HLSStreamer) distributeFrames() {
	defer func() {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	}()

	subID := fmt.Sprintf("stream_preview_%s_%d", s.capturer.GetID(), time.Now().UnixNano())
	frameChannel := s.capturer.SubscribeFrames(subID)
	defer s.capturer.UnsubscribeFrames(subID)

	for {
		select {
		case <-s.ctx.Done():
			return
		case frame, ok := <-frameChannel:
			if !ok {
				return
			}

			// 分发帧到所有订阅者
			s.subscriberMutex.RLock()
			for _, ch := range s.subscribers {
				select {
				case ch <- frame:
				default:
					// 通道满了，跳过
				}
			}
			s.subscriberMutex.RUnlock()
		}
	}
}

// StreamManager 流管理器
type StreamManager struct {
	streamers      map[string]*HLSStreamer
	captureManager *capture.Manager
	streamConfig   config.StreamConfig
	mutex          sync.RWMutex
}

// NewStreamManager 创建流管理器
func NewStreamManager(capManager *capture.Manager, streamCfg config.StreamConfig) *StreamManager {
	return &StreamManager{
		streamers:      make(map[string]*HLSStreamer),
		captureManager: capManager,
		streamConfig:   streamCfg,
	}
}

// CreateStream 创建流
func (m *StreamManager) CreateStream(capturerID string) (*HLSStreamer, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if streamer, exists := m.streamers[capturerID]; exists {
		return streamer, nil
	}

	cap, err := m.captureManager.GetCapturer(capturerID)
	if err != nil {
		return nil, err
	}

	streamer := NewHLSStreamer(cap, m.streamConfig)
	m.streamers[capturerID] = streamer
	return streamer, nil
}

// GetStream 获取流
func (m *StreamManager) GetStream(capturerID string) (*HLSStreamer, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	streamer, exists := m.streamers[capturerID]
	if !exists {
		return nil, fmt.Errorf("流 %s 不存在", capturerID)
	}
	return streamer, nil
}

// StartStream 启动流
func (m *StreamManager) StartStream(ctx context.Context, capturerID string) error {
	streamer, err := m.CreateStream(capturerID)
	if err != nil {
		return err
	}
	return streamer.Start(ctx)
}

// StopStream 停止流
func (m *StreamManager) StopStream(capturerID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	streamer, exists := m.streamers[capturerID]
	if !exists {
		return fmt.Errorf("流 %s 不存在", capturerID)
	}

	return streamer.Stop()
}

// StartAll 启动所有流
func (m *StreamManager) StartAll(ctx context.Context) error {
	capturers := m.captureManager.GetAllCapturers()
	for _, cap := range capturers {
		if err := m.StartStream(ctx, cap.GetID()); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// StopAll 停止所有流
func (m *StreamManager) StopAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, streamer := range m.streamers {
		streamer.Stop()
	}
}
