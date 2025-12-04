package rtmp

import (
	"context"
	"fmt"
	"sync"

	"home-monitor/internal/capture"
	"home-monitor/internal/config"
)

// Manager RTMP 推流管理器
type Manager struct {
	captureManager *capture.Manager
	cameras        map[string]config.CameraConfig
	streamers      map[string]*Streamer
	frameFeeds     map[string]context.CancelFunc

	mutex sync.RWMutex
	ctx   context.Context
}

// NewManager 创建 RTMP 管理器
func NewManager(ctx context.Context, captureManager *capture.Manager, cameras []config.CameraConfig) *Manager {
	m := &Manager{
		captureManager: captureManager,
		cameras:        make(map[string]config.CameraConfig),
		streamers:      make(map[string]*Streamer),
		frameFeeds:     make(map[string]context.CancelFunc),
		ctx:            ctx,
	}

	for _, cam := range cameras {
		if cam.Enabled {
			m.cameras[cam.ID] = cam
		}
	}

	return m
}

// StartStream 启动 RTMP 推流
func (m *Manager) StartStream(cameraID, rtmpURL string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查是否已在推流
	if streamer, exists := m.streamers[cameraID]; exists && streamer.IsRunning() {
		return fmt.Errorf("摄像头 %s 已在推流中", cameraID)
	}

	// 获取摄像头配置
	camConfig, exists := m.cameras[cameraID]
	if !exists {
		return fmt.Errorf("摄像头不存在: %s", cameraID)
	}

	// 获取采集器
	capturer, err := m.captureManager.GetCapturer(cameraID)
	if err != nil {
		return fmt.Errorf("获取采集器失败: %w", err)
	}

	if !capturer.IsRunning() {
		return fmt.Errorf("采集器未运行: %s", cameraID)
	}

	// 创建推流器
	streamer := NewStreamer(cameraID, camConfig, rtmpURL)

	// 启动推流
	if err := streamer.Start(m.ctx); err != nil {
		return err
	}

	// 订阅视频帧流
	feedCtx, feedCancel := context.WithCancel(m.ctx)
	m.frameFeeds[cameraID] = feedCancel

	videoSubID := fmt.Sprintf("rtmp_video_%s", cameraID)
	frameCh := capturer.SubscribeFrames(videoSubID)

	go func() {
		defer capturer.UnsubscribeFrames(videoSubID)
		for {
			select {
			case <-feedCtx.Done():
				return
			case frame, ok := <-frameCh:
				if !ok {
					return
				}
				streamer.WriteFrame(frame)
			}
		}
	}()

	// 订阅音频流（如果支持）
	if capturer.HasAudio() {
		audioSubID := fmt.Sprintf("rtmp_audio_%s", cameraID)
		audioCh := capturer.SubscribeAudio(audioSubID)

		go func() {
			defer capturer.UnsubscribeAudio(audioSubID)
			for {
				select {
				case <-feedCtx.Done():
					return
				case audio, ok := <-audioCh:
					if !ok {
						return
					}
					streamer.WriteAudio(audio)
				}
			}
		}()
	}

	m.streamers[cameraID] = streamer
	return nil
}

// StopStream 停止 RTMP 推流
func (m *Manager) StopStream(cameraID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 取消帧订阅
	if cancelFn, exists := m.frameFeeds[cameraID]; exists {
		cancelFn()
		delete(m.frameFeeds, cameraID)
	}

	// 停止推流器
	if streamer, exists := m.streamers[cameraID]; exists {
		streamer.Stop()
		delete(m.streamers, cameraID)
	}

	return nil
}

// GetStreamStatus 获取推流状态
func (m *Manager) GetStreamStatus(cameraID string) (bool, string) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if streamer, exists := m.streamers[cameraID]; exists && streamer.IsRunning() {
		return true, streamer.GetURL()
	}
	return false, ""
}

// GetAllStreams 获取所有推流
func (m *Manager) GetAllStreams() map[string]string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	streams := make(map[string]string)
	for id, streamer := range m.streamers {
		if streamer.IsRunning() {
			streams[id] = streamer.GetURL()
		}
	}
	return streams
}

// StopAll 停止所有推流
func (m *Manager) StopAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, cancelFn := range m.frameFeeds {
		cancelFn()
		delete(m.frameFeeds, id)
	}

	for id, streamer := range m.streamers {
		streamer.Stop()
		delete(m.streamers, id)
	}
}
