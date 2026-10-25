// Package update provides on-demand release checks and bounded, verified downloads.
package update

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const releaseURL = "https://api.github.com/repos/Leoforever123/Polysync/releases/latest"
const assetPrefix = "https://github.com/Leoforever123/Polysync/releases/download/"
const maxPackageSize int64 = 512 << 20
const assetName = "polysync-windows-amd64.msi"

type State struct {
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Phase      string `json:"phase"`
	Message    string `json:"message"`
	Available  bool   `json:"available"`
	Supported  bool   `json:"supported"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Token      string `json:"token"`
}
type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}
type candidate struct {
	version   string
	msi, sums asset
}
type Manager struct {
	mu                        sync.Mutex
	state                     State
	candidate                 candidate
	ctx                       context.Context
	cancel                    context.CancelFunc
	wg                        sync.WaitGroup
	client                    *http.Client
	host, cache, path, digest string
	install                   func(string, string) error
}

func New(ctx context.Context, current, cache, host string, install func(string, string) error) (*Manager, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{ctx: ctx, cancel: cancel, cache: cache, host: host, install: install, state: State{Current: current, Phase: "idle", Message: "点击检查更新", Supported: runtime.GOOS == "windows" && runtime.GOARCH == "amd64", Token: hex.EncodeToString(token)}}
	m.client = &http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("更新下载重定向过多")
		}
		if !trustedURL(req.URL) {
			return errors.New("更新下载地址不受信任")
		}
		return nil
	}}
	if data, err := os.ReadFile(filepath.Join(cache, "last-result.json")); err == nil {
		var r InstallResult
		if json.Unmarshal(data, &r) == nil {
			m.state.Message = r.Message
			if !r.Success {
				m.state.Phase = "error"
			}
		}
	}
	return m, nil
}
func (m *Manager) Close()          { m.mu.Lock(); m.cancel(); m.mu.Unlock(); m.wg.Wait() }
func (m *Manager) Snapshot() State { m.mu.Lock(); defer m.mu.Unlock(); return m.state }
func version(s string) ([3]int, error) {
	var v [3]int
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, errors.New("版本号必须是 major.minor.patch")
	}
	for i, p := range parts {
		if p == "" || len(p) > 5 || (len(p) > 1 && p[0] == '0') {
			return v, errors.New("版本号无效")
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return v, errors.New("版本号无效")
			}
		}
		n, e := strconv.Atoi(p)
		if e != nil || n > 65535 {
			return v, errors.New("版本号无效")
		}
		v[i] = n
	}
	return v, nil
}
func newer(a, b string) (bool, error) {
	va, e := version(a)
	if e != nil {
		return false, e
	}
	vb, e := version(b)
	if e != nil {
		return false, e
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i], nil
		}
	}
	return false, nil
}
func trustedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func (m *Manager) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
	if e != nil {
		return nil, e
	}
	if !trustedURL(req.URL) {
		return nil, errors.New("更新地址不受信任")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PolySync/"+m.state.Current)
	res, e := m.client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("更新服务器返回 HTTP %d，请稍后重试", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, errors.New("更新信息过大")
	}
	return b, nil
}
func (m *Manager) busy() bool {
	return m.state.Phase == "checking" || m.state.Phase == "downloading" || m.state.Phase == "installing"
}
func (m *Manager) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Phase = "error"
	m.state.Message = err.Error()
}
func (m *Manager) Check(ctx context.Context) error {
	m.mu.Lock()
	if m.busy() || m.ctx.Err() != nil {
		m.mu.Unlock()
		return errors.New("更新任务进行中或服务正在退出")
	}
	m.state.Phase = "checking"
	m.state.Message = "正在检查更新…"
	m.state.Available = false
	m.state.Latest = ""
	m.candidate = candidate{}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	err := m.check(ctx)
	if err != nil {
		m.fail(err)
	}
	return err
}
func (m *Manager) check(ctx context.Context) error {
	b, e := m.get(ctx, releaseURL, 2<<20)
	if e != nil {
		return e
	}
	var r release
	if e = json.Unmarshal(b, &r); e != nil {
		return errors.New("更新信息格式无效")
	}
	if r.Draft || r.Prerelease {
		return errors.New("没有可用的正式版本")
	}
	isNew, e := newer(r.Tag, m.state.Current)
	if e != nil {
		return fmt.Errorf("无法比较版本：%w", e)
	}
	c := candidate{version: strings.TrimPrefix(r.Tag, "v")}
	for _, a := range r.Assets {
		if a.Name == assetName {
			c.msi = a
		}
		if a.Name == "SHA256SUMS.txt" {
			c.sums = a
		}
	}
	if isNew && m.state.Supported {
		prefix := assetPrefix + r.Tag + "/"
		if c.msi.URL != prefix+assetName || c.sums.URL != prefix+"SHA256SUMS.txt" || c.msi.Size <= 0 || c.msi.Size > maxPackageSize {
			return errors.New("新版尚未提供有效的 Windows MSI 安装包和校验文件")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candidate = c
	m.state.Latest = c.version
	m.state.Available = isNew
	m.state.Phase = "current"
	m.state.Message = "已是最新版本"
	if isNew {
		m.state.Phase = "available"
		m.state.Message = "发现新版本，可下载并安装"
	}
	if !m.state.Supported {
		m.state.Message += "；当前系统暂不支持应用内安装"
	}
	return nil
}
func (m *Manager) Download() error {
	m.mu.Lock()
	if m.busy() || m.ctx.Err() != nil || !m.state.Available || !m.state.Supported || m.candidate.msi.URL == "" {
		m.mu.Unlock()
		return errors.New("请先检查可安装的新版本")
	}
	c := m.candidate
	m.state.Phase = "downloading"
	m.state.Message = "正在下载安装包…"
	m.state.Downloaded = 0
	m.state.Total = c.msi.Size
	m.path = ""
	m.digest = ""
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		if e := m.download(c); e != nil {
			m.fail(e)
		}
	}()
	return nil
}
func checksum(data []byte) (string, error) {
	var found string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == assetName {
			if found != "" {
				return "", errors.New("安装包校验值重复")
			}
			b, e := hex.DecodeString(f[0])
			if e != nil || len(b) != sha256.Size {
				return "", errors.New("安装包校验值无效")
			}
			found = strings.ToLower(f[0])
		}
	}
	if found == "" {
		return "", errors.New("缺少安装包校验值")
	}
	return found, nil
}
func (m *Manager) download(c candidate) error {
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Minute)
	defer cancel()
	data, e := m.get(ctx, c.sums.URL, 64<<10)
	if e != nil {
		return e
	}
	digest, e := checksum(data)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(m.cache, 0700); e != nil {
		return e
	}
	dir, e := os.MkdirTemp(m.cache, "download-")
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, assetName)
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	req, e := http.NewRequestWithContext(ctx, "GET", c.msi.URL, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "PolySync/"+m.state.Current)
	res, e := m.client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("下载安装包失败：HTTP %d", res.StatusCode)
	}
	hash := sha256.New()
	reader := io.LimitReader(res.Body, c.msi.Size+1)
	buf := make([]byte, 64<<10)
	var n int64
	for {
		count, readErr := reader.Read(buf)
		if count > 0 {
			n += int64(count)
			if n > c.msi.Size {
				return errors.New("安装包超过声明大小")
			}
			if _, e = io.MultiWriter(f, hash).Write(buf[:count]); e != nil {
				return e
			}
			m.mu.Lock()
			m.state.Downloaded = n
			m.mu.Unlock()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if n != c.msi.Size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("安装包校验失败，请重新下载")
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	m.mu.Lock()
	m.path = path
	m.digest = digest
	m.state.Phase = "ready"
	m.state.Message = "下载和校验完成，可安装并重启"
	m.mu.Unlock()
	ok = true
	return nil
}
func (m *Manager) Install() error {
	m.mu.Lock()
	if m.state.Phase != "ready" || m.install == nil || m.ctx.Err() != nil {
		m.mu.Unlock()
		return errors.New("请先下载并校验安装包")
	}
	path, digest := m.path, m.digest
	m.state.Phase = "installing"
	m.state.Message = "正在准备安装，应用将重启…"
	m.mu.Unlock()
	err := VerifyFile(path, digest)
	if err == nil {
		err = m.install(path, digest)
	}
	if err != nil {
		m.mu.Lock()
		m.state.Phase = "ready"
		m.state.Message = err.Error()
		m.mu.Unlock()
	}
	return err
}
func VerifyFile(path, digest string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, maxPackageSize+1))
	if e != nil {
		return e
	}
	if n > maxPackageSize || hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("安装包校验失败，请重新下载")
	}
	return nil
}
func (m *Manager) Routes(mux *http.ServeMux) {
	handler := func(op string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			host, _, e := net.SplitHostPort(r.RemoteAddr)
			ip := net.ParseIP(host)
			if e != nil || ip == nil || !ip.IsLoopback() || !strings.EqualFold(r.Host, m.host) {
				w.WriteHeader(403)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "仅允许本机控制台更新"})
				return
			}
			s := m.Snapshot()
			if op != "status" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-PolySync-Update-Token")), []byte(s.Token)) != 1 {
				w.WriteHeader(403)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "更新令牌无效，请重新打开更新窗口"})
				return
			}
			var err error
			switch op {
			case "check":
				err = m.Check(r.Context())
			case "download":
				err = m.Download()
			case "install":
				err = m.Install()
			}
			if err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(m.Snapshot())
		}
	}
	mux.HandleFunc("GET /api/update", handler("status"))
	for _, op := range []string{"check", "download", "install"} {
		mux.HandleFunc("POST /api/update/"+op, handler(op))
	}
}

type InstallResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
