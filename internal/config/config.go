package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	Server  ServerConfig   `yaml:"server"`
	Cameras []CameraConfig `yaml:"cameras"`
	Storage StorageConfig  `yaml:"storage"`
	Stream  StreamConfig   `yaml:"stream"`
	Preview PreviewConfig  `yaml:"preview"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// PreviewConfig 实时预览配置
type PreviewConfig struct {
	// MJPEG 配置
	MJPEG MJPEGConfig `yaml:"mjpeg"`
	// WebRTC 配置
	WebRTC WebRTCConfig `yaml:"webrtc"`
}

// MJPEGConfig MJPEG 流配置
type MJPEGConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`    // MJPEG 服务独立端口
	Quality int  `yaml:"quality"` // JPEG 质量 1-31，越小越好
}

// WebRTCConfig WebRTC 配置
type WebRTCConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Port       int      `yaml:"port"`         // WebRTC 服务独立端口
	STUNServer []string `yaml:"stun_servers"` // STUN 服务器列表
}

// CameraConfig 摄像头配置
type CameraConfig struct {
	ID          string      `yaml:"id"`
	Name        string      `yaml:"name"`
	Type        string      `yaml:"type"` // usb, rtsp, hls, file
	DeviceIndex int         `yaml:"device_index"`
	RTSPUrl     string      `yaml:"rtsp_url"`
	HLSUrl      string      `yaml:"hls_url"` // HLS/m3u8 流地址
	Width       int         `yaml:"width"`
	Height      int         `yaml:"height"`
	FPS         int         `yaml:"fps"`
	Enabled     bool        `yaml:"enabled"`
	Audio       AudioConfig `yaml:"audio"`
}

// AudioConfig 音频配置
type AudioConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Type        string `yaml:"type"`         // usb, pulse, alsa, avfoundation
	DeviceIndex int    `yaml:"device_index"` // 音频设备索引
	DeviceName  string `yaml:"device_name"`  // 音频设备名称 (Windows/macOS)
	SampleRate  int    `yaml:"sample_rate"`  // 采样率，默认 44100
	Channels    int    `yaml:"channels"`     // 声道数，默认 2
}

// StorageConfig 存储配置
type StorageConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Path            string `yaml:"path"`
	SegmentDuration string `yaml:"segment_duration"` // 支持: 300, "5m", "1h", "1h30m"
	RetentionDays   int    `yaml:"retention_days"`
	Format          string `yaml:"format"`
}

// StreamConfig 流配置
type StreamConfig struct {
	HLSSegmentDuration int    `yaml:"hls_segment_duration"`
	HLSPlaylistLength  int    `yaml:"hls_playlist_length"`
	TempPath           string `yaml:"temp_path"`
}

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// 设置默认值
	setDefaults(&config)

	return &config, nil
}

// ParseDuration 解析时间字符串，支持: "300"(秒), "5m", "1h", "1h30m", "1d"
func ParseDuration(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	// 尝试直接解析为数字（秒）
	if seconds, err := strconv.Atoi(s); err == nil {
		return seconds, nil
	}
	// 处理 "d" (天) - Go 的 time.ParseDuration 不支持天
	if strings.HasSuffix(s, "d") {
		prefix := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(prefix)
		if err == nil {
			return days * 86400, nil
		}
	}
	// 使用 Go 标准库解析 (支持 "5m", "1h", "1h30m" 等)
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("无效的时间格式: %s", s)
	}
	return int(d.Seconds()), nil
}

// GetSegmentDurationSeconds 获取分段时长（秒）
func (c *StorageConfig) GetSegmentDurationSeconds() int {
	seconds, err := ParseDuration(c.SegmentDuration)
	if err != nil || seconds <= 0 {
		return 300 // 默认 5 分钟
	}
	return seconds
}

// setDefaults 设置默认值
func setDefaults(config *Config) {
	if config.Server.Host == "" {
		config.Server.Host = "0.0.0.0"
	}
	if config.Server.Port == 0 {
		config.Server.Port = 8080
	}
	if config.Storage.Path == "" {
		config.Storage.Path = "./recordings"
	}
	if config.Storage.SegmentDuration == "" {
		config.Storage.SegmentDuration = "5m"
	}
	if config.Storage.RetentionDays == 0 {
		config.Storage.RetentionDays = 7
	}
	if config.Storage.Format == "" {
		config.Storage.Format = "mp4"
	}
	if config.Stream.HLSSegmentDuration == 0 {
		config.Stream.HLSSegmentDuration = 2
	}
	if config.Stream.HLSPlaylistLength == 0 {
		config.Stream.HLSPlaylistLength = 5
	}
	if config.Stream.TempPath == "" {
		config.Stream.TempPath = "./temp"
	}

	// 音频默认值
	for i := range config.Cameras {
		if config.Cameras[i].Audio.SampleRate == 0 {
			config.Cameras[i].Audio.SampleRate = 44100
		}
		if config.Cameras[i].Audio.Channels == 0 {
			config.Cameras[i].Audio.Channels = 2
		}
	}

	// 预览默认值
	// 默认启用 MJPEG
	if !config.Preview.MJPEG.Enabled && !config.Preview.WebRTC.Enabled {
		config.Preview.MJPEG.Enabled = true
	}
	if config.Preview.MJPEG.Port == 0 {
		config.Preview.MJPEG.Port = 8081
	}
	if config.Preview.MJPEG.Quality == 0 {
		config.Preview.MJPEG.Quality = 5
	}
	if config.Preview.WebRTC.Port == 0 {
		config.Preview.WebRTC.Port = 8082
	}
	if len(config.Preview.WebRTC.STUNServer) == 0 {
		config.Preview.WebRTC.STUNServer = []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		}
	}
}
