package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"everevo/internal/httpclient"
	fileatomic "everevo/internal/atomic"
)

var dlHTTPClient = httpclient.New(0)

const segCount = 4

// ProgressEvent 进度事件数据。
type ProgressEvent struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	Status   string `json:"status"` // downloading / paused / completed / error
	Written  int64  `json:"written"`
	Total    int64  `json:"total"`
	Speed    int64  `json:"speed"` // bytes/sec
	Pct      int    `json:"pct"`
	Reason   string `json:"reason"` // 失败原因（status=error 时）
}

// TaskInfo is the frontend-facing representation of a download task.
type TaskInfo struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	DestPath    string `json:"destPath"`
	Status      string `json:"status"`
	Written     int64  `json:"written"`
	Total       int64  `json:"total"`
	Speed       int64  `json:"speed"`
	Pct         int    `json:"pct"`
	Reason      string `json:"reason,omitempty"`
	CreatedAt   int64  `json:"createdAt"`   // unix millis
	CompletedAt int64  `json:"completedAt"` // unix millis, 0 if active
}

// EmitFn 进度推送回调（接 Wails EventsEmit）。
type EmitFn func(event string, data interface{})

type segment struct {
	Start   int64 `json:"start"`
	End     int64 `json:"end"`
	Written int64 `json:"written"`
}

// Retryable errors are transient network issues worth retrying automatically.
var retryableErrors = []string{
	"timeout", "connection reset", "connection refused",
	"temporary failure", "no such host", "EOF", "broken pipe",
	"503", "502", "504", "429",
}

func isRetryable(reason string) bool {
	lower := strings.ToLower(reason)
	for _, p := range retryableErrors {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// retryDelays defines the backoff between auto-retry attempts.
var retryDelays = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}

const defaultMaxRetries = 3
const defaultMaxConcurrent = 3

// Task 一个下载任务。
type Task struct {
	ID          string            `json:"id"`
	URL         string            `json:"-"`
	DestPath    string            `json:"destPath"`
	Filename    string            `json:"filename"`
	Source      string            `json:"-"`    // catalog source name for auth injection
	Headers     map[string]string `json:"-"`    // extra HTTP headers (cookies, auth)
	Total       int64             `json:"total"`
	Status      string            `json:"status"`
	reason      string            // 失败原因
	CreatedAt   int64             `json:"createdAt"`   // unix millis
	CompletedAt int64             `json:"completedAt"` // unix millis
	segs        []segment
	cancel      context.CancelFunc
	mu          sync.Mutex
	lastWrite   int64
	lastSpeed   int64   // bytes/sec, set by emitProgressWithSpeed
	written     int64   // written bytes for non-segmented (tryDownload) path
	emit        EmitFn
	mgr         *Manager // back-reference for moving to history
	retryCount  int       // number of retries attempted
	maxRetries  int       // max auto-retries before permanent failure
}

type Manager struct {
	tasks         map[string]*Task
	pending       []*Task   // queued tasks waiting for a slot
	history       []*Task   // completed / failed tasks, kept for display
	maxConcurrent int       // max simultaneous downloads (default 3)
	mu            sync.RWMutex
	emit          EmitFn
	historyPath   string // JSON file for persisting history across restarts
}

type persistedTask struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	DestPath    string `json:"destPath"`
	URL         string `json:"url"`
	Status      string `json:"status"`
	Total       int64  `json:"total"`
	Reason      string `json:"reason,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	CompletedAt int64  `json:"completedAt"`
}

func NewManager(emit EmitFn) *Manager {
	return &Manager{
		tasks:         map[string]*Task{},
		history:       []*Task{},
		maxConcurrent: defaultMaxConcurrent,
		emit:          emit,
	}
}

// SetMaxConcurrent configures the download concurrency limit.
func (m *Manager) SetMaxConcurrent(n int) {
	if n < 1 {
		n = 1
	}
	m.mu.Lock()
	m.maxConcurrent = n
	m.mu.Unlock()
}

// activeCount returns the number of actively downloading tasks (not paused/queued).
// Caller must hold m.mu (at least RLock).
func (m *Manager) activeCount() int {
	count := 0
	for _, t := range m.tasks {
		if t.Status == "downloading" {
			count++
		}
	}
	return count
}

// dequeueNext starts the next pending task if a slot is available.
func (m *Manager) dequeueNext() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.activeCount() < m.maxConcurrent && len(m.pending) > 0 {
		next := m.pending[0]
		m.pending = m.pending[1:]
		next.Status = "downloading"
		next.emitProgress()
		go next.run()
	}
}

// SetHistoryPath configures the file used to persist download history.
func (m *Manager) SetHistoryPath(path string) {
	m.historyPath = path
}

// LoadHistory reads persisted history from disk.
func (m *Manager) LoadHistory() {
	if m.historyPath == "" {
		return
	}
	data, err := os.ReadFile(m.historyPath)
	if err != nil {
		return
	}
	var persisted []persistedTask
	if err := json.Unmarshal(data, &persisted); err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range persisted {
		m.history = append(m.history, &Task{
			ID: p.ID, Filename: p.Filename, DestPath: p.DestPath, URL: p.URL,
			Status: p.Status, Total: p.Total, reason: p.Reason,
			CreatedAt: p.CreatedAt, CompletedAt: p.CompletedAt,
		})
	}
}

// saveHistory writes current history to disk.
func (m *Manager) saveHistory() {
	if m.historyPath == "" {
		return
	}
	m.mu.RLock()
	var persisted []persistedTask
	for _, t := range m.history {
		persisted = append(persisted, persistedTask{
			ID: t.ID, Filename: t.Filename, DestPath: t.DestPath, URL: t.URL,
			Status: t.Status, Total: t.Total, Reason: t.reason,
			CreatedAt: t.CreatedAt, CompletedAt: t.CompletedAt,
		})
	}
	m.mu.RUnlock()
	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		log.Printf("[下载] 保存历史 JSON 失败: %v", err)
		return
	}
	if err := fileatomic.WriteFile(m.historyPath, data, 0644); err != nil {
		log.Printf("[下载] 保存历史失败: %v", err)
	}
}

func (m *Manager) GetTask(id string) *Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tasks[id]
}

// Start 启动下载（自动检测是否支持断点续传/多段）。
// If max concurrent downloads are already running, the task is queued.
func (m *Manager) Start(id, url, destPath, filename string) *Task {
	task := &Task{
		ID: id, URL: url, DestPath: destPath, Filename: filename,
		Status: "downloading", CreatedAt: time.Now().UnixMilli(), emit: m.emit, mgr: m,
		maxRetries: defaultMaxRetries,
	}
	m.mu.Lock()
	m.tasks[id] = task
	if m.activeCount() >= m.maxConcurrent {
		task.Status = "queued"
		m.pending = append(m.pending, task)
		// Emit queued status so frontend shows "排队中"
		task.emitProgress()
	} else {
		go task.run()
	}
	m.mu.Unlock()
	return task
}

// retryTask is called internally by the auto-retry timer.
func (m *Manager) retryTask(t *Task) {
	m.mu.Lock()
	t.Status = "downloading"
	m.mu.Unlock()
	go t.run()
}

// Retry re-queues a failed download from history.
func (m *Manager) Retry(id string) {
	m.mu.Lock()
	var found *Task
	for i, t := range m.history {
		if t.ID == id {
			found = t
			m.history = append(m.history[:i], m.history[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	if found == nil || found.URL == "" {
		return
	}
	// Start fresh download with same parameters.
	m.Start(id, found.URL, found.DestPath, found.Filename)
}

// moveToHistory moves a completed/failed task from active map to history.
func (m *Manager) moveToHistory(id string) {
	m.mu.Lock()
	if t, ok := m.tasks[id]; ok {
		t.CompletedAt = time.Now().UnixMilli()
		m.history = append(m.history, t)
		delete(m.tasks, id)
	}
	m.mu.Unlock()
	m.saveHistory()
	// Free up a slot for the next queued download.
	m.dequeueNext()
}

// List returns all active (non-terminal) tasks.
func (m *Manager) List() []TaskInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []TaskInfo
	for _, t := range m.tasks {
		list = append(list, t.info())
	}
	return list
}

// History returns completed and failed tasks, newest first.
func (m *Manager) History() []TaskInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []TaskInfo
	for i := len(m.history) - 1; i >= 0; i-- {
		list = append(list, m.history[i].info())
	}
	return list
}

// Remove cancels (if active) and removes a task from both active and history lists.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	if t, ok := m.tasks[id]; ok {
		if t.cancel != nil { t.cancel() }
		delete(m.tasks, id)
	}
	// Also remove from pending queue
	for i, t := range m.pending {
		if t.ID == id {
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			break
		}
	}
	for i, t := range m.history {
		if t.ID == id {
			m.history = append(m.history[:i], m.history[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	m.saveHistory()
	// Removing an active task frees a slot.
	m.dequeueNext()
}

func (m *Manager) ClearHistory() {
	m.mu.Lock()
	m.history = []*Task{}
	m.mu.Unlock()
	m.saveHistory()
}

// info returns a TaskInfo snapshot of a Task.
func (t *Task) info() TaskInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	written := t.totalWritten()
	pct := 0
	if t.Total > 0 {
		pct = int(written * 100 / t.Total)
	}
	if t.Status == "completed" {
		pct = 100
	}
	return TaskInfo{
		ID:          t.ID,
		Filename:    t.Filename,
		DestPath:    t.DestPath,
		Status:      t.Status,
		Written:     written,
		Total:       t.Total,
		Speed:       t.lastSpeed,
		Pct:         pct,
		Reason:      t.reason,
		CreatedAt:   t.CreatedAt,
		CompletedAt: t.CompletedAt,
	}
}

// Pause 暂停下载。
func (m *Manager) Pause(id string) {
	m.mu.RLock()
	task := m.tasks[id]
	m.mu.RUnlock()
	if task != nil {
		task.pause()
	}
}

// Resume 恢复下载（如果并发已满则排队）。
func (m *Manager) Resume(id string) {
	m.mu.Lock()
	task := m.tasks[id]
	if task != nil && task.Status == "paused" {
		if m.activeCount() >= m.maxConcurrent {
			task.Status = "queued"
			task.emitProgress()
			// Insert at front of pending queue
			m.pending = append([]*Task{task}, m.pending...)
		} else {
			task.Status = "downloading"
			go task.run()
		}
	}
	m.mu.Unlock()
}

func (t *Task) metaPath() string { return t.DestPath + ".meta" }
func (t *Task) partPath() string { return t.DestPath + ".part" }

// fail 标记任务失败。Auto-retries on transient errors.
func (t *Task) fail(reason string) {
	if t.retryCount < t.maxRetries && isRetryable(reason) {
		t.retryCount++
		delayIdx := t.retryCount - 1
		if delayIdx >= len(retryDelays) {
			delayIdx = len(retryDelays) - 1
		}
		delay := retryDelays[delayIdx]
		t.reason = reason
		log.Printf("[下载] 失败: %s %s (重试 %d/%d)", t.Filename, reason, t.retryCount+1, t.maxRetries)
		t.emit("download-progress", ProgressEvent{
			ID: t.ID, File: t.Filename, Status: "retrying",
			Written: t.totalWritten(), Total: t.Total,
			Reason: fmt.Sprintf("重试 %d/%d (%v 后)", t.retryCount, t.maxRetries, delay),
		})
		// Clean up partial file before retry
		os.Remove(t.partPath())
		os.Remove(t.metaPath())
		atomic.StoreInt64(&t.written, 0)
		t.segs = nil
		time.AfterFunc(delay, func() { t.mgr.retryTask(t) })
		return
	}
	t.Status = "error"
	t.reason = reason
	log.Printf("[下载] 错误: %s %s", t.Filename, reason)
	t.emitProgress()
	t.mgr.moveToHistory(t.ID)
}

func (t *Task) pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
	t.Status = "paused"
	t.saveMeta()
	t.emitProgress()
}

// saveMeta/loadMeta 断点续传元数据（同时支持分段和单连接两种模式）。
func (t *Task) saveMeta() {
	data, err := json.Marshal(struct {
		Total   int64     `json:"total"`
		Segs    []segment `json:"segs"`
		Written int64     `json:"written"`
	}{t.Total, t.segs, atomic.LoadInt64(&t.written)})
	if err != nil {
		log.Printf("[下载] 保存元数据 JSON 失败: %v", err)
		return
	}
	if err := fileatomic.WriteFile(t.metaPath(), data, 0644); err != nil {
		log.Printf("[下载] 保存元数据失败: %v", err)
	}
}

func (t *Task) loadMeta() bool {
	data, err := os.ReadFile(t.metaPath())
	if err != nil {
		return false
	}
	var m struct {
		Total   int64     `json:"total"`
		Segs    []segment `json:"segs"`
		Written int64     `json:"written"`
	}
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	t.Total = m.Total
	t.segs = m.Segs
	atomic.StoreInt64(&t.written, m.Written)
	// Only segmented resumes use this path; tryDownload handles its own resume.
	return len(t.segs) > 0
}

func (t *Task) applyHeaders(req *http.Request) {
	if req == nil || len(t.Headers) == 0 {
		return
	}
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	// Set a default User-Agent if none provided.
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "everevo/0.1")
	}
}

func (t *Task) doHead(url string) (*http.Response, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	t.applyHeaders(req)
	return dlHTTPClient.Do(req)
}

func (t *Task) run() {
	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()

	// HEAD 获取文件大小
	resp, err := t.doHead(t.URL)
	if err != nil {
		// HEAD 失败也可能是暂时的，尝试单连接下载
		t.tryDownload(ctx)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		t.tryDownload(ctx) // HEAD not supported → fall back to simple GET
		return
	}
	total := resp.ContentLength
	supportRange := resp.Header.Get("Accept-Ranges") == "bytes"
	resp.Body.Close()

	if !supportRange || total < 1024*1024 {
		t.tryDownload(ctx) // 小文件/不支持 Range → 单连接
		return
	}

	t.Total = total
	// 加载断点
	if !t.loadMeta() {
		// If a partial tryDownload exists (.part file), resume through that path.
		if info, statErr := os.Stat(t.partPath()); statErr == nil && info.Size() > 0 {
			t.tryDownload(ctx)
			return
		}
		// 新下载：分段
		segSize := total / int64(segCount)
		t.segs = make([]segment, segCount)
		for i := 0; i < segCount; i++ {
			s := int64(i) * segSize
			e := s + segSize - 1
			if i == segCount-1 {
				e = total - 1
			}
			t.segs[i] = segment{Start: s, End: e}
		}
		// 预分配文件
		f, err := os.Create(t.partPath())
		if err != nil {
			t.fail("创建文件失败: " + err.Error())
			return
		}
		f.Truncate(total)
		f.Close()
	}

	// 多段并发
	var wg sync.WaitGroup
	var speedCounter int64

	for i := range t.segs {
		if t.segs[i].Written >= (t.segs[i].End - t.segs[i].Start + 1) {
			continue // 已完成
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			t.downloadSeg(ctx, idx, &speedCounter)
		}(i)
	}

	// 进度 ticker
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w := t.totalWritten()
				t.lastWrite = atomic.LoadInt64(&speedCounter)
				atomic.StoreInt64(&speedCounter, 0)
				t.emitProgressWithSpeed(w, t.lastWrite)
				t.saveMeta()
			}
		}
	}()

	wg.Wait()

	if ctx.Err() != nil {
		// 被取消（暂停）
		t.saveMeta()
		return
	}

	// 合并：.part → 目标
	os.Remove(t.metaPath())
	os.Rename(t.partPath(), t.DestPath)
	t.Status = "completed"
	t.emitProgress()
	t.mgr.moveToHistory(t.ID)
}

func (t *Task) totalWritten() int64 {
	if len(t.segs) == 0 {
		return atomic.LoadInt64(&t.written)
	}
	var w int64
	for _, s := range t.segs {
		w += s.Written
	}
	return w
}

func (t *Task) downloadSeg(ctx context.Context, idx int, counter *int64) {
	seg := &t.segs[idx]
	startByte := seg.Start + seg.Written
	if startByte > seg.End {
		return
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", t.URL, nil)
	t.applyHeaders(req)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", startByte, seg.End))
	resp, err := dlHTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(t.partPath(), os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Seek(startByte, 0)

	buf := make([]byte, 64*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			t.mu.Lock()
			seg.Written += int64(n)
			t.mu.Unlock()
			atomic.AddInt64(counter, int64(n))
		}
		if err != nil {
			break
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// tryDownload 回退：单连接下载（不支持 Range 或小文件）。
// Writes to .part file; renames to DestPath on success.
// Supports resume via Range header if .part already exists.
func (t *Task) tryDownload(ctx context.Context) {
	// ── Resume check: if .part exists, try to continue ──
	partInfo, statErr := os.Stat(t.partPath())
	resume := statErr == nil && partInfo.Size() > 0

	req, _ := http.NewRequestWithContext(ctx, "GET", t.URL, nil)
	t.applyHeaders(req)
	if resume {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", partInfo.Size()))
	}
	resp, err := dlHTTPClient.Do(req)
	if err != nil {
		t.fail("请求失败: " + err.Error())
		return
	}
	defer resp.Body.Close()

	// Handle resume response
	if resume {
		if resp.StatusCode == http.StatusPartialContent {
			// Server supports Range — append to existing .part
			t.Total = partInfo.Size() + resp.ContentLength
			atomic.StoreInt64(&t.written, partInfo.Size())
		} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Server doesn't support Range — restart
			resume = false
			atomic.StoreInt64(&t.written, 0)
			os.Remove(t.partPath())
			t.Total = resp.ContentLength
		} else if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			log.Printf("[下载] 续传 416: %s（文件大小已变化，从头下载）", t.Filename)
			resume = false
			atomic.StoreInt64(&t.written, 0)
			os.Remove(t.partPath())
		} else {
			t.fail(fmt.Sprintf("服务器返回 HTTP %d", resp.StatusCode))
			return
		}
	} else {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			t.fail(fmt.Sprintf("服务器返回 HTTP %d", resp.StatusCode))
			return
		}
		t.Total = resp.ContentLength
		atomic.StoreInt64(&t.written, 0)
	}

	// Open .part file (append if resuming, create otherwise)
	var flag int
	if resume {
		flag = os.O_WRONLY | os.O_APPEND
	} else {
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	out, err := os.OpenFile(t.partPath(), flag, 0644)
	if err != nil {
		t.fail("创建文件失败: " + err.Error())
		return
	}
	defer out.Close()

	buf := make([]byte, 32*1024)
	var speedCounter int64

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				w := atomic.LoadInt64(&t.written)
				t.emitProgressWithSpeed(w, atomic.SwapInt64(&speedCounter, 0))
				t.saveMeta()
			}
		}
	}()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			atomic.AddInt64(&t.written, int64(n))
			atomic.AddInt64(&speedCounter, int64(n))
		}
		if err != nil {
			break
		}
		if ctx.Err() != nil {
			close(done)
			t.Status = "paused"
			t.emitProgress()
			t.saveMeta()
			return
		}
	}
	close(done)

	if ctx.Err() == nil {
		t.Status = "completed"
		t.emitProgressWithSpeed(atomic.LoadInt64(&t.written), 0)
		os.Remove(t.metaPath())
		os.Rename(t.partPath(), t.DestPath)
		t.mgr.moveToHistory(t.ID)
	}
}

func (t *Task) emitProgress() {
	t.emitProgressWithSpeed(t.totalWritten(), 0)
}

func (t *Task) emitProgressWithSpeed(written, speed int64) {
	t.lastSpeed = speed
	pct := 0
	if t.Total > 0 {
		pct = int(written * 100 / t.Total)
	}
	t.emit("download-progress", ProgressEvent{
		ID: t.ID, File: t.Filename, Status: t.Status,
		Written: written, Total: t.Total, Speed: speed, Pct: pct,
		Reason: t.reason,
	})
}
