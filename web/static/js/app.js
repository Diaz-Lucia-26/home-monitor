// 家庭监控系统前端应用

class HomeMonitor {
    constructor() {
        this.cameras = [];
        this.recordings = [];
        this.currentTab = 'live';
        this.init();
    }

    init() {
        this.setupTabs();
        this.loadCameras();
        this.loadRecordings();
        this.updateTime();
        this.checkSystemStatus();
        
        // 定时更新
        setInterval(() => this.updateTime(), 1000);
        setInterval(() => this.checkSystemStatus(), 5000);
        
        // 事件监听
        document.getElementById('refresh-recordings')?.addEventListener('click', () => this.loadRecordings());
        document.getElementById('close-player')?.addEventListener('click', () => this.closePlayer());
        document.getElementById('recording-camera-select')?.addEventListener('change', () => this.loadRecordings());
    }

    // 标签切换
    setupTabs() {
        const tabs = document.querySelectorAll('.tab-btn');
        tabs.forEach(tab => {
            tab.addEventListener('click', () => {
                tabs.forEach(t => t.classList.remove('active'));
                tab.classList.add('active');
                
                const tabName = tab.dataset.tab;
                this.currentTab = tabName;
                
                document.querySelectorAll('.tab-content').forEach(content => {
                    content.classList.remove('active');
                });
                document.getElementById(`${tabName}-section`)?.classList.add('active');
                
                // 切换到录像标签时刷新列表
                if (tabName === 'recordings') {
                    this.loadRecordings();
                }
            });
        });
    }

    // 更新时间
    updateTime() {
        const now = new Date();
        const timeStr = now.toLocaleString('zh-CN', {
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit'
        });
        document.getElementById('current-time').textContent = timeStr;
    }

    // 检查系统状态
    async checkSystemStatus() {
        try {
            const response = await fetch('/api/status');
            const data = await response.json();
            
            if (data.success) {
                const statusEl = document.getElementById('system-status');
                const running = data.data.running_cameras;
                const total = data.data.total_cameras;
                statusEl.textContent = `${running}/${total} 在线`;
                statusEl.classList.add('online');
                statusEl.classList.remove('offline');
            }
        } catch (error) {
            const statusEl = document.getElementById('system-status');
            statusEl.textContent = '离线';
            statusEl.classList.add('offline');
            statusEl.classList.remove('online');
        }
    }

    // 加载摄像头列表
    async loadCameras() {
        try {
            const response = await fetch('/api/cameras');
            const data = await response.json();
            
            if (data.success) {
                this.cameras = data.data || [];
                this.renderCameras();
                this.renderCameraSettings();
                this.updateCameraSelect();
            }
        } catch (error) {
            console.error('加载摄像头失败:', error);
            this.showToast('加载摄像头失败', 'error');
        }
    }

    // 渲染摄像头
    renderCameras() {
        const container = document.getElementById('cameras-container');
        if (!container) return;

        if (this.cameras.length === 0) {
            container.innerHTML = `
                <div class="empty-state">
                    <p>📷</p>
                    <p>暂无摄像头</p>
                </div>
            `;
            return;
        }

        container.innerHTML = this.cameras.map(camera => `
            <div class="camera-card" id="camera-${camera.id}">
                <div class="camera-header">
                    <div class="camera-name">
                        <span class="camera-status ${camera.is_running ? 'running' : ''}"></span>
                        ${camera.name}
                    </div>
                    <div class="camera-controls">
                        <button class="btn btn-icon btn-secondary" onclick="app.takeSnapshot('${camera.id}')" title="截图">📷</button>
                        <button class="btn btn-icon btn-secondary" onclick="app.toggleFullscreen('${camera.id}')" title="全屏">⛶</button>
                    </div>
                </div>
                <div class="camera-view" id="camera-view-${camera.id}">
                    ${camera.is_running ? `
                        <img src="/api/stream/${camera.id}/mjpeg" alt="Camera ${camera.id}" 
                             onerror="this.onerror=null; this.parentElement.innerHTML='<div class=\\'camera-placeholder\\'><p>⚠️</p><p>视频加载失败</p></div>'" />
                    ` : `
                        <div class="camera-placeholder">
                            <p>📹</p>
                            <p>等待连接</p>
                        </div>
                    `}
                </div>
                <div class="camera-footer">
                    <span>${camera.id}</span>
                    <span>${camera.is_running ? '● 在线' : '○ 离线'}</span>
                </div>
            </div>
        `).join('');
    }

    // 渲染摄像头设置
    renderCameraSettings() {
        const container = document.getElementById('camera-settings-list');
        if (!container) return;

        if (this.cameras.length === 0) {
            container.innerHTML = `
                <div class="empty-state">
                    <p>暂无摄像头配置</p>
                </div>
            `;
            return;
        }

        container.innerHTML = this.cameras.map(camera => `
            <div class="camera-setting-item">
                <div class="camera-setting-info">
                    <h4>${camera.name}</h4>
                    <p>ID: ${camera.id} · ${camera.is_running ? '运行中' : '已停止'}</p>
                </div>
                <div>
                    <span class="camera-status ${camera.is_running ? 'running' : ''}" style="display: inline-block;"></span>
                </div>
            </div>
        `).join('');
    }

    // 更新摄像头选择器
    updateCameraSelect() {
        const select = document.getElementById('recording-camera-select');
        if (!select) return;

        select.innerHTML = '<option value="">全部</option>' + 
            this.cameras.map(camera => 
                `<option value="${camera.id}">${camera.name}</option>`
            ).join('');
    }

    // 截图
    async takeSnapshot(id) {
        try {
            const response = await fetch(`/api/cameras/${id}/snapshot`);
            if (response.ok) {
                const blob = await response.blob();
                const url = URL.createObjectURL(blob);
                
                const a = document.createElement('a');
                a.href = url;
                a.download = `snapshot_${id}_${Date.now()}.jpg`;
                a.click();
                
                URL.revokeObjectURL(url);
                this.showToast('截图已保存', 'success');
            } else {
                this.showToast('截图失败', 'error');
            }
        } catch (error) {
            console.error('截图失败:', error);
            this.showToast('截图失败', 'error');
        }
    }

    // 全屏
    toggleFullscreen(id) {
        const view = document.getElementById(`camera-view-${id}`);
        if (view) {
            if (document.fullscreenElement) {
                document.exitFullscreen();
            } else {
                view.requestFullscreen();
            }
        }
    }

    // 加载录像列表
    async loadRecordings() {
        try {
            const cameraId = document.getElementById('recording-camera-select')?.value || '';
            const date = document.getElementById('recording-date')?.value || '';
            
            let url = '/api/recordings';
            const params = new URLSearchParams();
            if (cameraId) params.append('camera_id', cameraId);
            if (date) {
                params.append('start_time', new Date(date).toISOString());
                params.append('end_time', new Date(date + 'T23:59:59').toISOString());
            }
            if (params.toString()) url += '?' + params.toString();
            
            const response = await fetch(url);
            const data = await response.json();
            
            if (data.success) {
                this.recordings = data.data || [];
                this.renderRecordings();
            }
        } catch (error) {
            console.error('加载录像失败:', error);
            this.showToast('加载录像失败', 'error');
        }
    }

    // 渲染录像列表
    renderRecordings() {
        const container = document.getElementById('recordings-list');
        if (!container) return;

        if (this.recordings.length === 0) {
            container.innerHTML = `
                <div class="empty-state">
                    <p>📁</p>
                    <p>暂无录像</p>
                </div>
            `;
            return;
        }

        container.innerHTML = this.recordings.map(rec => `
            <div class="recording-item">
                <div class="recording-info">
                    <div class="recording-name">${rec.file_name}</div>
                    <div class="recording-meta">
                        ${rec.camera_id} · ${new Date(rec.start_time).toLocaleString('zh-CN')} · ${this.formatSize(rec.size)}
                    </div>
                </div>
                <div class="recording-actions">
                    <button class="btn btn-primary btn-sm" onclick="app.playRecording('${rec.camera_id}', '${rec.file_name}')">播放</button>
                    <button class="btn btn-secondary btn-sm" onclick="app.downloadRecording('${rec.camera_id}', '${rec.file_name}')">下载</button>
                    <button class="btn btn-danger btn-sm" onclick="app.deleteRecording('${rec.camera_id}', '${rec.file_name}')">删除</button>
                </div>
            </div>
        `).join('');
    }

    // 播放录像
    playRecording(cameraId, filename) {
        const container = document.getElementById('video-player-container');
        const player = document.getElementById('video-player');
        
        if (container && player) {
            player.src = `/api/recordings/${cameraId}/${filename}`;
            container.style.display = 'block';
            player.play();
        }
    }

    // 关闭播放器
    closePlayer() {
        const container = document.getElementById('video-player-container');
        const player = document.getElementById('video-player');
        
        if (container && player) {
            player.pause();
            player.src = '';
            container.style.display = 'none';
        }
    }

    // 下载录像
    downloadRecording(cameraId, filename) {
        window.open(`/api/recordings/${cameraId}/${filename}/download`, '_blank');
    }

    // 删除录像
    async deleteRecording(cameraId, filename) {
        if (!confirm('确定要删除这个录像吗？')) return;
        
        try {
            const response = await fetch(`/api/recordings/${cameraId}/${filename}`, {
                method: 'DELETE'
            });
            const data = await response.json();
            
            if (data.success) {
                this.showToast('已删除', 'success');
                this.loadRecordings();
            } else {
                this.showToast(data.error || '删除失败', 'error');
            }
        } catch (error) {
            console.error('删除录像失败:', error);
            this.showToast('删除录像失败', 'error');
        }
    }

    // 格式化文件大小
    formatSize(bytes) {
        if (!bytes) return '0 B';
        const units = ['B', 'KB', 'MB', 'GB'];
        let i = 0;
        while (bytes >= 1024 && i < units.length - 1) {
            bytes /= 1024;
            i++;
        }
        return `${bytes.toFixed(1)} ${units[i]}`;
    }

    // 显示提示消息
    showToast(message, type = 'info') {
        // 移除已有的 toast
        document.querySelectorAll('.toast').forEach(t => t.remove());
        
        const toast = document.createElement('div');
        toast.className = `toast ${type}`;
        toast.textContent = message;
        document.body.appendChild(toast);
        
        setTimeout(() => {
            toast.style.opacity = '0';
            toast.style.transform = 'translateY(8px)';
            setTimeout(() => toast.remove(), 200);
        }, 2500);
    }
}

// 初始化应用
const app = new HomeMonitor();
