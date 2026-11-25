// Package mobile embeds the existing PolySync service without a browser, tray,
// HTTP listener, updater or periodic sync loop. Only metadata crosses the binding.
package mobile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"polysync/internal/discovery"
	"polysync/internal/model"
	"polysync/internal/rootidentity"
	"polysync/internal/service"
	"polysync/internal/store"
	"polysync/internal/syncer"
)

type Engine struct {
	discovery   *discovery.Service
	mu          sync.RWMutex
	workspaceMu sync.Mutex
	svc         *service.Service
	handler     http.Handler
	dir         string
	cancel      context.CancelFunc
}

func NewEngine() *Engine { return &Engine{} }

// Start creates a fresh service each time; Stop is idempotent.
func (e *Engine) Start(directory, name, listen string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.svc != nil {
		return nil
	}
	if !filepath.IsAbs(directory) {
		return errors.New("应用数据目录必须是绝对路径")
	}
	for _, sub := range []string{"state", "imports", "workspaces"} {
		if err := os.MkdirAll(filepath.Join(directory, sub), 0700); err != nil {
			return err
		}
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(directory, "state"), listen, "127.0.0.1:0")
	if err != nil {
		return err
	}
	e.discovery = discovery.NewExternal(st.Config().DeviceID)
	svc := service.NewWithOptions(st, service.Options{Discovery: e.discovery, DisableDiscovery: true, DisableAutoSync: true})
	ctx, cancel := context.WithCancel(context.Background())
	if err = svc.Start(ctx); err != nil {
		cancel()
		svc.Stop()
		return err
	}
	e.svc, e.handler, e.dir, e.cancel = svc, svc.Handler(), directory, cancel
	if name != "" {
		body, _ := json.Marshal(map[string]string{"deviceName": name})
		_, err = e.call("PUT", "/api/settings", string(body))
		if err != nil {
			cancel()
			svc.Stop()
			e.svc = nil
			e.handler = nil
			return err
		}
	}
	return nil
}

func (e *Engine) Stop() {
	e.mu.RLock()
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.RUnlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.svc != nil {
		e.cancel()
		e.svc.Stop()
		e.svc = nil
		e.handler = nil
	}
}

// DiscoveryInfo exposes only public identity for the platform's DNS-SD TXT data.
func (e *Engine) DiscoveryInfo() (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return "", errors.New("服务未启动")
	}
	c := e.svc.Store().Config()
	b, err := json.Marshal(map[string]any{"id": c.DeviceID, "name": c.DeviceName, "key": c.IdentityPublicKey, "fp": discovery.Fingerprint(c.IdentityPublicKey), "version": model.ProtocolVersion, "port": e.svc.ListenPort()})
	return string(b), err
}

// UpdateDiscovery replaces untrusted address hints; pairing still checks TLS
// against the advertised key and requires approval plus the six-digit code.
func (e *Engine) UpdateDiscovery(snapshot string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return errors.New("服务未启动")
	}
	if len(snapshot) > 128<<10 {
		return errors.New("发现结果过大")
	}
	var devices []model.NearbyDevice
	if err := json.Unmarshal([]byte(snapshot), &devices); err != nil {
		return err
	}
	return e.discovery.ReplaceExternal(devices)
}

// Request dispatches a small, explicitly allowed set of existing JSON APIs in
// process. It never exposes desktop filesystem dialogs or an unauthenticated port.
func (e *Engine) Request(method, path, body string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return "", errors.New("服务未启动")
	}
	if !allowed(method, path) {
		return "", errors.New("移动端不支持此操作")
	}
	if len(body) > 64<<10 {
		return "", errors.New("请求过大")
	}
	return e.call(method, path, body)
}

func allowed(method, path string) bool {
	if !strings.HasPrefix(path, "/api/") || strings.ContainsAny(path, "?#%\\") || strings.Contains(path, "..") || strings.Contains(path, "//") || strings.HasSuffix(path, "/") {
		return false
	}
	if method == "GET" && (path == "/api/status" || path == "/api/transfers") {
		return true
	}
	if method == "POST" && (path == "/api/pair/start" || path == "/api/pair/confirm") {
		return true
	}
	p := strings.Split(strings.Trim(path, "/"), "/")
	if len(p) == 3 && p[0] == "api" && p[1] == "paired-devices" && safeName(p[2]) {
		return method == "DELETE"
	}
	if len(p) == 4 && p[0] == "api" && p[1] == "pair" && p[2] == "requests" {
		return method == "DELETE"
	}
	if len(p) == 5 && p[0] == "api" && p[1] == "pair" && p[2] == "requests" && p[4] == "approve" {
		return method == "POST"
	}
	if len(p) >= 4 && p[1] == "shares" && safeName(p[2]) {
		if len(p) == 5 && p[3] == "members" && safeName(p[4]) {
			return method == "PUT" || method == "DELETE"
		}
		if len(p) == 4 {
			switch p[3] {
			case "sync", "members", "writeback-confirm":
				return method == "POST"
			case "writeback-preview":
				return method == "GET"
			}
		}
	}
	return len(p) == 4 && p[0] == "api" && p[1] == "transfers" && (p[3] == "cancel" || p[3] == "retry") && method == "POST"
}

func (e *Engine) call(method, path, body string) (string, error) {
	r, err := http.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	if err != nil {
		return "", errors.New("请求地址无效")
	}
	r.RemoteAddr = "127.0.0.1:0"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	if w.Code >= 400 {
		var result struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) == nil && result.Error != "" {
			return "", errors.New(result.Error)
		}
		return "", fmt.Errorf("操作失败：HTTP %d", w.Code)
	}
	return w.Body.String(), nil
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00") && len(name) < 240
}

// SendImport consumes a native-staged file using bounded streaming into the
// existing upload pipeline. Native code owns and removes its imports afterwards.
func (e *Engine) SendImport(peerID, importName, displayName string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return "", errors.New("服务未启动")
	}
	if !safeName(importName) || !safeName(displayName) {
		return "", errors.New("文件名无效")
	}
	root, err := os.OpenRoot(filepath.Join(e.dir, "imports"))
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(importName)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("仅支持普通文件")
	}
	task, err := e.svc.CreateTransfer(peerID, []service.TransferFile{{Name: displayName, Size: info.Size()}})
	if err != nil {
		return "", err
	}
	reader, writer := io.Pipe()
	multi := multipart.NewWriter(writer)
	finished := make(chan error, 1)
	go func() {
		part, writeErr := multi.CreateFormFile("file", displayName)
		if writeErr == nil {
			_, writeErr = io.Copy(part, f)
		}
		if writeErr == nil {
			writeErr = multi.Close()
		}
		_ = writer.CloseWithError(writeErr)
		finished <- writeErr
	}()
	err = e.svc.UploadTransfer(context.Background(), task.ID, multipart.NewReader(reader, multi.Boundary()), reader)
	_ = reader.Close()
	writeErr := <-finished
	if err != nil {
		return "", err
	}
	if writeErr != nil {
		return "", writeErr
	}
	return task.ID, nil
}

// CreateWorkspace creates only an app-managed synchronization root.
func (e *Engine) CreateWorkspace(name, peerID, invitationID string) (string, error) {
	return e.CreateWorkspaceWithRole(name, peerID, invitationID, "", false)
}

// CreateWorkspaceWithRole invites a paired device with folder-specific permissions.
// When accepting, permissions come from the authenticated invitation.
func (e *Engine) CreateWorkspaceWithRole(name, peerID, invitationID, role string, allowWriteBack bool) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return "", errors.New("服务未启动")
	}
	e.workspaceMu.Lock()
	defer e.workspaceMu.Unlock()
	if !safeName(name) {
		return "", errors.New("工作区名称无效")
	}
	if invitationID != "" {
		var shareID string
		for _, inv := range e.svc.ShareInvitations() {
			if inv.ID == invitationID {
				shareID = inv.ShareID
				break
			}
		}
		if shareID == "" || !safeName(invitationID) || strings.ContainsAny(invitationID, "?#%") {
			return "", errors.New("邀请无效")
		}
		if _, exists := e.svc.Store().Share(shareID); exists {
			path, err := e.workspace(shareID)
			if err != nil {
				return "", err
			}
			body, _ := json.Marshal(map[string]any{"path": path, "autoSync": false, "intervalSeconds": 30})
			return e.call("POST", "/api/share-invitations/"+invitationID+"/accept", string(body))
		}
	} else if (role != "" && role != model.RoleCollaborator && role != model.RoleFollower) || (allowWriteBack && role != model.RoleFollower) {
		return "", errors.New("工作区成员权限无效")
	}
	path := filepath.Join(e.dir, "workspaces", store.RandomID())
	if err := os.Mkdir(path, 0700); err != nil {
		return "", err
	}
	// Enroll this specific new directory before sending. Its token lets a failed
	// request remove only its own marker, even when a pathname is later reused.
	binding, err := rootidentity.Bind(path)
	if err != nil {
		_ = os.Remove(path) // only an empty directory
		return "", err
	}
	invite := map[string]any{"path": path, "name": name, "deviceId": peerID, "autoSync": false, "intervalSeconds": 30}
	if role != "" {
		invite["role"], invite["allowWriteBack"] = role, allowWriteBack
	}
	body, _ := json.Marshal(invite)
	endpoint := "/api/shares/invite"
	if invitationID != "" {
		endpoint = "/api/share-invitations/" + invitationID + "/accept"
		body, _ = json.Marshal(map[string]any{"path": path, "autoSync": false, "intervalSeconds": 30})
	}
	result, err := e.call("POST", endpoint, string(body))
	if err != nil {
		// An interrupted confirmation can leave a pending membership for retry.
		for _, share := range e.svc.Store().Config().Shares {
			if share.Path == path || share.RootID == binding {
				return "", err
			}
		}
		return "", errors.Join(err, rootidentity.RemoveUnusedBinding(path, binding))
	}
	return result, err
}

func (e *Engine) workspace(id string) (string, error) {
	root, err := e.openWorkspace(id)
	if err != nil {
		return "", err
	}
	defer root.Close()
	return root.Name(), nil
}

func (e *Engine) openWorkspace(id string) (*os.Root, error) {
	share, ok := e.svc.Store().Share(id)
	if !ok {
		return nil, errors.New("工作区不存在")
	}
	// Android exposes aliases of the private data directory. Accept either
	// parent spelling; the service verifies the workspace itself on the handle.
	managed, err := os.Stat(filepath.Join(e.dir, "workspaces"))
	if err != nil {
		return nil, err
	}
	parent, err := os.Stat(filepath.Dir(share.Path))
	if err != nil {
		return nil, err
	}
	if !os.SameFile(parent, managed) || !safeName(filepath.Base(share.Path)) {
		return nil, errors.New("工作区不属于此应用")
	}
	return e.svc.OpenWorkspaceRoot(id, share.Path)
}

func (e *Engine) WorkspaceFiles(id string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return "", errors.New("服务未启动")
	}
	root, err := e.openWorkspace(id)
	if err != nil {
		return "", err
	}
	defer root.Close()
	files := []map[string]any{}
	err = fs.WalkDir(root.FS(), ".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && rootidentity.Reserved(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("工作区包含符号链接")
		}
		if d.IsDir() || syncer.IsTemporaryFileName(d.Name()) {
			return nil
		}
		if len(files) >= 2000 {
			return errors.New("文件较多，请在电脑上查看")
		}
		info, err := root.Lstat(filepath.FromSlash(p))
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files = append(files, map[string]any{"name": p, "size": info.Size()})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(files)
	return string(data), err
}

func (e *Engine) ImportToWorkspace(id, importName, displayName string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return errors.New("服务未启动")
	}
	if !safeName(importName) || !safeName(displayName) || rootidentity.Reserved(displayName) || syncer.IsTemporaryFileName(displayName) {
		return errors.New("文件名无效")
	}
	dstRoot, err := e.openWorkspace(id)
	if err != nil {
		return err
	}
	defer dstRoot.Close()
	share, _ := e.svc.Store().Share(id)
	if share.LocalRole == model.RoleFollower {
		canWrite := false
		for _, member := range share.Members {
			if member.State == "active" && member.AllowWriteBack && !member.WriteBackPending {
				canWrite = true
				break
			}
		}
		if !canWrite {
			return errors.New("此工作区只接收副本")
		}
	}
	srcRoot, err := os.OpenRoot(filepath.Join(e.dir, "imports"))
	if err != nil {
		return err
	}
	defer srcRoot.Close()
	src, err := srcRoot.Open(importName)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := dstRoot.OpenFile(displayName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("无法导入，文件可能已存在：%w", err)
	}
	_, err = io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil {
		_ = dstRoot.Remove(displayName)
		return err
	}
	return closeErr
}

// ExportPath returns only validated completed receipt/workspace files to the
// trusted native shell, which streams them into ACTION_CREATE_DOCUMENT's URI.
func (e *Engine) ExportPath(taskID string, index int) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return "", errors.New("服务未启动")
	}
	result, err := e.call("GET", "/api/transfers", "")
	if err != nil {
		return "", err
	}
	var list struct {
		Tasks []service.TransferTask `json:"tasks"`
	}
	if err = json.Unmarshal([]byte(result), &list); err != nil {
		return "", err
	}
	for _, task := range list.Tasks {
		if task.ID != taskID {
			continue
		}
		if task.Direction != "receive" || task.State != "complete" || index < 0 || index >= len(task.Files) {
			return "", errors.New("文件尚未接收完成")
		}
		return filepath.Join(task.StorageDir, task.Files[index].Blob), nil
	}
	return "", errors.New("任务不存在")
}

func (e *Engine) WorkspaceExportPath(id, name string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.svc == nil {
		return "", errors.New("服务未启动")
	}
	if !filepath.IsLocal(name) || syncer.ValidateRelativePath(filepath.ToSlash(name)) != nil || syncer.IsTemporaryFileName(filepath.Base(name)) {
		return "", errors.New("路径无效")
	}
	root, err := e.openWorkspace(id)
	if err != nil {
		return "", err
	}
	defer root.Close()
	// Workspaces do not synchronize symbolic links. Refuse aliases to hidden
	// markers as well as links leaving the workspace.
	current := "."
	for part := range strings.SplitSeq(filepath.ToSlash(name), "/") {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("工作区包含符号链接")
		}
	}
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<30 {
		return "", errors.New("仅支持不超过 16 GiB 的普通文件")
	}
	// Android opens the returned path after its document picker closes. Use the
	// same immutable object cache as sync so a later mount cannot redirect it.
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(f, info.Size()+1))
	if err != nil {
		return "", err
	}
	after, err := root.Lstat(name)
	if err != nil {
		return "", err
	}
	if size != info.Size() || after.Size() != size || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
		return "", errors.New("文件已更改，请重新导出")
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	st := e.svc.Store()
	if err := st.PutObject(root, model.Entry{Path: filepath.ToSlash(name), Hash: hash, Size: size}); err != nil {
		return "", err
	}
	return st.ObjectPath(hash), nil
}

// HasActiveWork is a small native bridge call, without status serialization.
func (e *Engine) HasActiveWork() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.svc != nil && e.svc.HasActiveWork()
}
