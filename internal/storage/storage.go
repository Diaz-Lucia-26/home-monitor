package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"home-monitor/internal/capture"
	"home-monitor/internal/config"
)

// Recording 录像信息
type Recording struct {
	ID        string    `json:"id"`
	CameraID  string    `json:"camera_id"`
	FileName  string    `json:"file_name"`
	FilePath  string    `json:"file_path"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  int       `json:"duration"`
	Size      int64     `json:"size"`
}

// StorageManager 存储管理器
// 注意：录像功能现在由 FFmpeg segment 在 capturer 中自动处理
// StorageManager 主要负责：录像文件查询、删除、清理过期文件
type StorageManager struct {
	captureManager *capture.Manager
	config         config.StorageConfig
	mutex          sync.RWMutex
}

// NewStorageManager 创建存储管理器
func NewStorageManager(capManager *capture.Manager, cfg config.StorageConfig) *StorageManager {
	return &StorageManager{
		captureManager: capManager,
		config:         cfg,
	}
}

// StartAll 启动所有录像（兼容旧接口，实际录像由 capturer 处理）
func (m *StorageManager) StartAll(ctx context.Context) error {
	// 录像现在由 FFmpeg segment 在 capturer 中自动处理
	// 这里只需要确保目录存在
	capturers := m.captureManager.GetAllCapturers()
	for _, cap := range capturers {
		cameraPath := filepath.Join(m.config.Path, cap.GetID())
		if err := os.MkdirAll(cameraPath, 0755); err != nil {
			return fmt.Errorf("创建录像目录失败: %w", err)
		}
	}
	return nil
}

// StopAll 停止所有录像（兼容旧接口）
func (m *StorageManager) StopAll() {
	// 录像由 capturer 控制，这里无需操作
}

// GetRecordings 获取录像列表
func (m *StorageManager) GetRecordings(capturerID string, startTime, endTime time.Time) ([]Recording, error) {
	var recordings []Recording

	cameraPath := filepath.Join(m.config.Path, capturerID)
	if _, err := os.Stat(cameraPath); os.IsNotExist(err) {
		return recordings, nil
	}

	files, err := os.ReadDir(cameraPath)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		if !strings.HasSuffix(name, "."+m.config.Format) {
			continue
		}

		// 解析文件名格式: cam1_20060102_150405.mp4
		parts := strings.Split(strings.TrimSuffix(name, "."+m.config.Format), "_")
		if len(parts) < 3 {
			continue
		}

		dateStr := parts[1] + "_" + parts[2]
		fileTime, err := time.ParseInLocation("20060102_150405", dateStr, time.Local)
		if err != nil {
			continue
		}

		if !startTime.IsZero() && fileTime.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && fileTime.After(endTime) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		recordings = append(recordings, Recording{
			ID:        fmt.Sprintf("%s_%d", capturerID, fileTime.Unix()),
			CameraID:  capturerID,
			FileName:  name,
			FilePath:  filepath.Join(cameraPath, name),
			StartTime: fileTime,
			Size:      info.Size(),
		})
	}

	sort.Slice(recordings, func(i, j int) bool {
		return recordings[i].StartTime.After(recordings[j].StartTime)
	})

	return recordings, nil
}

// GetAllRecordings 获取所有录像
func (m *StorageManager) GetAllRecordings() ([]Recording, error) {
	var allRecordings []Recording

	capturers := m.captureManager.GetAllCapturers()
	for _, cap := range capturers {
		recordings, err := m.GetRecordings(cap.GetID(), time.Time{}, time.Time{})
		if err != nil {
			continue
		}
		allRecordings = append(allRecordings, recordings...)
	}

	sort.Slice(allRecordings, func(i, j int) bool {
		return allRecordings[i].StartTime.After(allRecordings[j].StartTime)
	})

	return allRecordings, nil
}

// DeleteRecording 删除录像
func (m *StorageManager) DeleteRecording(filePath string) error {
	return os.Remove(filePath)
}

// CleanupOldRecordings 清理过期录像
func (m *StorageManager) CleanupOldRecordings() error {
	cutoffTime := time.Now().AddDate(0, 0, -m.config.RetentionDays)

	capturers := m.captureManager.GetAllCapturers()
	for _, cap := range capturers {
		recordings, err := m.GetRecordings(cap.GetID(), time.Time{}, cutoffTime)
		if err != nil {
			continue
		}

		for _, rec := range recordings {
			if err := m.DeleteRecording(rec.FilePath); err != nil {
				log.Printf("删除录像失败: %s, 错误: %v", rec.FilePath, err)
			} else {
				log.Printf("已删除过期录像: %s", rec.FilePath)
			}
		}
	}

	return nil
}

// StartCleanupTask 启动清理任务
func (m *StorageManager) StartCleanupTask(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	m.CleanupOldRecordings()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.CleanupOldRecordings()
		}
	}
}
