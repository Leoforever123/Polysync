package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"polysync/internal/store"
)

const transferMaxBytes int64 = 16 << 30
const transferMaxFiles = 128

type TransferFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Hash  string `json:"hash,omitempty"`
	Blob  string `json:"blob,omitempty"`
	State string `json:"state"`
}
type TransferTask struct {
	ID         string         `json:"id"`
	PeerID     string         `json:"peerId"`
	PeerName   string         `json:"peerName"`
	Direction  string         `json:"direction"`
	State      string         `json:"state"`
	Message    string         `json:"message,omitempty"`
	Files      []TransferFile `json:"files"`
	Total      int64          `json:"total"`
	Done       int64          `json:"done"`
	Created    time.Time      `json:"created"`
	Updated    time.Time      `json:"updated"`
	StorageDir string         `json:"storageDir"`
}
type TransferSettings struct {
	AutoReceive bool   `json:"autoReceive"`
	Directory   string `json:"directory"`
}
type transferManager struct {
	mu       sync.Mutex
	dir      string
	settings TransferSettings
	tasks    map[string]*TransferTask
	cancels  map[string]context.CancelFunc
	notify   func(string, string)
}

func newTransferManager(dir string) *transferManager {
	dir, _ = filepath.Abs(dir)
	return &transferManager{dir: dir, settings: TransferSettings{AutoReceive: true, Directory: filepath.Join(dir, "inbox")}, tasks: make(map[string]*TransferTask), cancels: make(map[string]context.CancelFunc)}
}
func transferActive(state string) bool {
	switch state {
	case "preparing", "connecting", "sending", "receiving", "verifying":
		return true
	}
	return false
}
func (m *transferManager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(m.dir, "transfers", "records"), 0700); err != nil {
		return err
	}
	settings, err := os.ReadFile(filepath.Join(m.dir, "transfer-settings.json"))
	if err == nil {
		if err = json.Unmarshal(settings, &m.settings); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !filepath.IsAbs(m.settings.Directory) {
		return errors.New("随传暂存目录必须是绝对路径")
	}
	entries, err := os.ReadDir(filepath.Join(m.dir, "transfers", "records"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, "transfers", "records", entry.Name()))
		if err != nil {
			return err
		}
		var task TransferTask
		if err := json.Unmarshal(data, &task); err != nil {
			return fmt.Errorf("随传记录损坏: %w", err)
		}
		if task.ID+".json" != entry.Name() {
			return errors.New("随传记录 ID 不匹配")
		}
		m.tasks[task.ID] = &task
		if transferActive(task.State) {
			task.State = "interrupted"
			task.Message = "应用重启，传输已中断，请重新发送"
			m.cleanIncompleteLocked(&task)
			if err := m.persistLocked(&task); err != nil {
				return err
			}
		}
	}
	return nil
}
func writeTransferJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".record-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func (m *transferManager) persistLocked(t *TransferTask) error {
	return writeTransferJSON(filepath.Join(m.dir, "transfers", "records", t.ID+".json"), t)
}
func (m *transferManager) Settings() TransferSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}
func canonicalTransferDir(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("请输入绝对路径")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", err
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(actual, ".polysync-write-check-*")
	if err != nil {
		return "", fmt.Errorf("暂存目录不可写: %w", err)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return filepath.Clean(actual), nil
}
func (s *Service) SetTransferSettings(settings TransferSettings) error {
	s.transferConfigMu.Lock()
	defer s.transferConfigMu.Unlock()
	dir, err := canonicalTransferDir(settings.Directory)
	if err != nil {
		return err
	}
	for _, share := range s.store.Config().Shares {
		path := share.Path
		if actual, e := filepath.EvalSymlinks(path); e == nil {
			path = actual
		}
		if pathsOverlap(dir, path) {
			return errors.New("随传暂存目录不能与同步文件夹重叠")
		}
	}
	settings.Directory = dir
	m := s.transfers
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := writeTransferJSON(filepath.Join(m.dir, "transfer-settings.json"), settings); err != nil {
		return err
	}
	m.settings = settings
	return nil
}
func validateTransferFiles(files []TransferFile) (int64, error) {
	if len(files) == 0 || len(files) > transferMaxFiles {
		return 0, fmt.Errorf("每次发送 1–%d 个普通文件", transferMaxFiles)
	}
	var total int64
	for _, file := range files {
		name := file.Name
		if name == "" || name == "." || name == ".." || len([]rune(name)) > 180 || strings.TrimRight(name, " .") != name || strings.ContainsAny(name, "/\\:\x00<>|?*\"") {
			return 0, errors.New("文件名无效或包含路径")
		}
		for _, r := range name {
			if r < 32 {
				return 0, errors.New("文件名包含控制字符")
			}
		}
		stem := strings.ToUpper(strings.Split(name, ".")[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
			return 0, errors.New("不支持系统保留文件名")
		}
		if file.Size < 0 || file.Size > transferMaxBytes-total {
			return 0, errors.New("每批文件总量不能超过 16 GiB")
		}
		total += file.Size
	}
	return total, nil
}
func (m *transferManager) create(peerID, peerName, direction string, files []TransferFile) (TransferTask, error) {
	total, err := validateTransferFiles(files)
	if err != nil {
		return TransferTask{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	active := 0
	for _, task := range m.tasks {
		if transferActive(task.State) {
			active++
		}
	}
	if active >= 4 {
		return TransferTask{}, errors.New("已有 4 个任务进行中，请稍后重试")
	}
	if direction == "receive" && !m.settings.AutoReceive {
		return TransferTask{}, errors.New("对方已关闭随传接收")
	}
	base := filepath.Join(m.dir, "transfers", "outgoing")
	if direction == "receive" {
		base = m.settings.Directory
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return TransferTask{}, err
	}
	free, err := transferFreeSpace(base)
	if err != nil {
		return TransferTask{}, err
	}
	// Reserve outstanding bytes conservatively across jobs; writes still handle ENOSPC.
	reserved := int64(0)
	for _, task := range m.tasks {
		if transferActive(task.State) {
			reserved += max(0, task.Total-task.Done)
		}
	}
	if uint64(total+reserved) > free {
		return TransferTask{}, errors.New("暂存空间不足")
	}
	id := store.RandomID()
	dir := filepath.Join(base, "polysync-"+id)
	if err := os.Mkdir(dir, 0700); err != nil {
		return TransferTask{}, err
	}
	task := TransferTask{ID: id, PeerID: peerID, PeerName: peerName, Direction: direction, State: "preparing", Total: total, Created: time.Now(), Updated: time.Now(), StorageDir: dir}
	if direction == "receive" {
		task.State = "receiving"
	}
	for i, f := range files {
		task.Files = append(task.Files, TransferFile{Name: f.Name, Size: f.Size, Hash: f.Hash, Blob: fmt.Sprintf("%03d-%s", i, f.Name), State: "pending"})
	}
	if err := m.persistLocked(&task); err != nil {
		os.Remove(dir)
		return TransferTask{}, err
	}
	m.tasks[id] = &task
	return cloneTransfer(task), nil
}
func cloneTransfer(t TransferTask) TransferTask {
	t.Files = append([]TransferFile(nil), t.Files...)
	return t
}
func (m *transferManager) Task(id string) (TransferTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return TransferTask{}, errors.New("传输记录不存在")
	}
	return cloneTransfer(*t), nil
}
func (m *transferManager) List() []TransferTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]TransferTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		copy := cloneTransfer(*t)
		for i, f := range copy.Files {
			if f.State == "complete" || f.State == "ready" {
				if file, err := transferFileOpen(copy, i); err != nil {
					copy.Files[i].State = "missing"
				} else {
					file.Close()
				}
			}
		}
		result = append(result, copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result
}
func transferFileOpen(t TransferTask, index int) (*os.File, error) {
	if index < 0 || index >= len(t.Files) {
		return nil, errors.New("文件不存在")
	}
	root, err := os.OpenRoot(t.StorageDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(t.Files[index].Blob)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("暂存文件无效")
	}
	return f, nil
}
func (m *transferManager) update(id, state, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return errors.New("传输记录不存在")
	}
	t.State = state
	t.Message = message
	t.Updated = time.Now()
	return m.persistLocked(t)
}
func (m *transferManager) progress(id string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tasks[id]; t != nil {
		t.Done += int64(n)
	}
}
func (m *transferManager) fileDone(id string, index int, hash, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	t.Files[index].Hash = hash
	t.Files[index].State = state
	t.Updated = time.Now()
	return m.persistLocked(t)
}
func (m *transferManager) bind(id string, cancel context.CancelFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil || !transferActive(t.State) {
		return errors.New("任务已经结束")
	}
	if _, exists := m.cancels[id]; exists {
		return errors.New("任务正在执行")
	}
	m.cancels[id] = cancel
	return nil
}
func (m *transferManager) cleanIncompleteLocked(t *TransferTask) {
	root, err := os.OpenRoot(t.StorageDir)
	if err != nil {
		return
	}
	defer root.Close()
	for i, f := range t.Files {
		_ = root.Remove(f.Blob + ".part")
		if f.State != "complete" && f.State != "ready" {
			_ = root.Remove(f.Blob)
			t.Files[i].State = "interrupted"
		}
	}
}
func (m *transferManager) finish(id string, err error) {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return
	}
	delete(m.cancels, id)
	if err != nil {
		t.State = "failed"
		t.Message = err.Error()
		if errors.Is(err, context.Canceled) {
			t.State = "cancelled"
			t.Message = "传输已取消"
		}
		m.cleanIncompleteLocked(t)
	} else {
		t.State = "complete"
		t.Done = t.Total
	}
	t.Updated = time.Now()
	if saveErr := m.persistLocked(t); saveErr != nil {
		t.State = "failed"
		t.Message = "保存传输记录失败：" + saveErr.Error()
	}
	notify := m.notify
	text := t.Message
	if t.State == "complete" {
		text = fmt.Sprintf("已%s %d 个文件 · %s", map[string]string{"send": "发送", "receive": "收到"}[t.Direction], len(t.Files), t.PeerName)
	}
	m.mu.Unlock()
	if notify != nil {
		notify(text, id)
	}
}
func (m *transferManager) Cancel(id string) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return errors.New("任务不存在")
	}
	cancel := m.cancels[id]
	if !transferActive(t.State) {
		m.mu.Unlock()
		return errors.New("任务已经结束")
	}
	if cancel != nil {
		cancel()
		m.mu.Unlock()
		return nil
	}
	t.State = "cancelled"
	t.Message = "传输已取消"
	m.cleanIncompleteLocked(t)
	err := m.persistLocked(t)
	m.mu.Unlock()
	return err
}
func (m *transferManager) Remove(id string, deleteFiles bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return errors.New("记录不存在")
	}
	if transferActive(t.State) {
		return errors.New("请先取消进行中的任务")
	}
	if deleteFiles {
		root, err := os.OpenRoot(t.StorageDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if root != nil {
			defer root.Close()
			for _, f := range t.Files {
				for _, name := range []string{f.Blob, f.Blob + ".part"} {
					if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
				}
			}
		}
	}
	if err := os.Remove(filepath.Join(m.dir, "transfers", "records", id+".json")); err != nil {
		return err
	}
	delete(m.tasks, id)
	return nil
}
func (m *transferManager) SaveCopy(id string, index int, destination string, overwrite bool) error {
	return m.saveCopyContext(context.Background(), id, index, destination, overwrite)
}
func (m *transferManager) saveCopyContext(ctx context.Context, id string, index int, destination string, overwrite bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	task, err := m.Task(id)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(task.Files) || task.Direction != "receive" || task.Files[index].State != "complete" {
		return errors.New("文件尚未接收完成")
	}
	source, err := transferFileOpen(task, index)
	if err != nil {
		return errors.New("暂存文件已不存在")
	}
	defer source.Close()
	if !filepath.IsAbs(destination) {
		return errors.New("保存路径必须是绝对路径")
	}
	if pathsOverlap(destination, filepath.Join(task.StorageDir, task.Files[index].Blob)) {
		return errors.New("请选择暂存原件以外的位置")
	}
	dest, err := os.CreateTemp(filepath.Dir(destination), ".polysync-save-*")
	if err != nil {
		return err
	}
	temp := dest.Name()
	defer os.Remove(temp)
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(dest, hash), transferCopyReader{ctx, source})
	if err == nil {
		err = dest.Sync()
	}
	closeErr := dest.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != task.Files[index].Size || hex.EncodeToString(hash.Sum(nil)) != task.Files[index].Hash {
		return errors.New("暂存文件已经变化，无法另存")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !overwrite {
		// Hard-link creation is exclusive: no race can overwrite an existing name.
		if err := os.Link(temp, destination); err != nil {
			return fmt.Errorf("无法保存（目标可能已存在，请改名或确认覆盖）: %w", err)
		}
		return nil
	}
	return os.Rename(temp, destination)
}

type transferCopyReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r transferCopyReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
