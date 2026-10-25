package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, payload []byte) (*Manager, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	m, e := New(context.Background(), "0.4.0", t.TempDir(), "127.0.0.1:45124", func(string, string) error { calls.Add(1); return nil })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	m.state.Supported = true
	hash := sha256.Sum256(payload)
	m.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		var b string
		switch r.URL.String() {
		case releaseURL:
			raw, _ := json.Marshal(release{Tag: "v0.4.1", Assets: []asset{{Name: assetName, URL: assetPrefix + "v0.4.1/" + assetName, Size: int64(len(payload))}, {Name: "SHA256SUMS.txt", URL: assetPrefix + "v0.4.1/SHA256SUMS.txt"}}})
			b = string(raw)
		case assetPrefix + "v0.4.1/SHA256SUMS.txt":
			b = hex.EncodeToString(hash[:]) + "  " + assetName
		case assetPrefix + "v0.4.1/" + assetName:
			b = string(payload)
		default:
			t.Errorf("unexpected URL: %s", r.URL)
			b = ""
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b)), Header: make(http.Header)}, nil
	})}
	return m, calls
}
func await(t *testing.T, m *Manager) State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot()
		if s.Phase != "downloading" {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("download timeout")
	return State{}
}
func TestVersionOrdering(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{{"v0.10.0", "0.9.9", true}, {"0.4.0", "v0.4.0", false}, {"0.3.9", "0.4.0", false}, {"1.0.0", "0.99.0", true}} {
		got, e := newer(tc.a, tc.b)
		if e != nil || got != tc.want {
			t.Fatalf("%+v: %v %v", tc, got, e)
		}
	}
	for _, s := range []string{"dev", "0.4", "0.4.1-beta", "01.2.3", "1.2.-3", "1.2.999999"} {
		if _, e := version(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestCheckDownloadAndInstall(t *testing.T) {
	m, calls := fixture(t, []byte("sample installer"))
	if e := m.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !m.Snapshot().Available {
		t.Fatal("update missing")
	}
	if e := m.Download(); e != nil {
		t.Fatal(e)
	}
	if s := await(t, m); s.Phase != "ready" || s.Downloaded != 16 {
		t.Fatalf("%+v", s)
	}
	if e := m.Install(); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("installer not launched")
	}
	if e := m.Install(); e == nil {
		t.Fatal("duplicate install accepted")
	}
}
func TestTamperedDownloadNeverInstalls(t *testing.T) {
	m, calls := fixture(t, []byte("installer"))
	if e := m.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := m.Download(); e != nil {
		t.Fatal(e)
	}
	await(t, m)
	if e := os.WriteFile(m.path, []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := m.Install(); e == nil || calls.Load() != 0 {
		t.Fatal("tampered file reached installer")
	}
}
func TestDownloadRejectsWrongSizeAndHash(t *testing.T) {
	for _, data := range []string{"bad", "longer than advertised"} {
		t.Run(data, func(t *testing.T) {
			m, calls := fixture(t, []byte("good"))
			if e := m.Check(context.Background()); e != nil {
				t.Fatal(e)
			}
			base := m.client.Transport
			m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				res, e := base.RoundTrip(r)
				if strings.HasSuffix(r.URL.Path, ".msi") {
					res.Body = io.NopCloser(strings.NewReader(data))
				}
				return res, e
			})
			if e := m.Download(); e != nil {
				t.Fatal(e)
			}
			if s := await(t, m); s.Phase != "error" {
				t.Fatalf("%+v", s)
			}
			if e := m.Install(); e == nil || calls.Load() != 0 {
				t.Fatal("bad download accepted")
			}
			entries, _ := os.ReadDir(m.cache)
			if len(entries) != 0 {
				t.Fatal("failed download retained")
			}
		})
	}
}
func TestRejectsUntrustedAssetsAndPrerelease(t *testing.T) {
	for _, r := range []release{{Tag: "v0.4.1", Prerelease: true}, {Tag: "v0.4.1", Draft: true}, {Tag: "v0.4.1", Assets: []asset{{Name: assetName, URL: "https://evil.invalid/update.msi", Size: 10}}}} {
		m, _ := fixture(t, []byte("test"))
		m.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			b, _ := json.Marshal(r)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
		})
		if e := m.Check(context.Background()); e == nil {
			t.Fatal("invalid release accepted")
		}
	}
}
func TestUpdateEndpointsRequireLocalHostAndToken(t *testing.T) {
	m, _ := fixture(t, []byte("test"))
	mux := http.NewServeMux()
	m.Routes(mux)
	for _, tc := range []struct {
		method, path, host, remote, token string
		want                              int
	}{{"GET", "/api/update", m.host, "127.0.0.1:1234", "", 200}, {"GET", "/api/update", "evil.invalid", "127.0.0.1:1234", "", 403}, {"GET", "/api/update", m.host, "192.168.1.2:1234", "", 403}, {"POST", "/api/update/check", m.host, "127.0.0.1:1234", "", 403}, {"POST", "/api/update/check", m.host, "127.0.0.1:1234", m.state.Token, 200}} {
		req := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, nil)
		req.RemoteAddr = tc.remote
		req.Header.Set("X-PolySync-Update-Token", tc.token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
}
func TestDownloadCancellationAndBusyInstall(t *testing.T) {
	m, _ := fixture(t, []byte("test"))
	if e := m.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	m.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	if e := m.Download(); e != nil {
		t.Fatal(e)
	}
	if e := m.Check(context.Background()); e == nil {
		t.Fatal("check while downloading")
	}
	m.Close()
	if s := m.Snapshot(); s.Phase != "error" {
		t.Fatalf("%+v", s)
	}
}
func TestHelperFailureRestartsPreviousAndSuccessStartsInstalled(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "failure", false: "success"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "update.msi")
			data := []byte("installer")
			if e := os.WriteFile(p, data, 0600); e != nil {
				t.Fatal(e)
			}
			h := sha256.Sum256(data)
			j := Job{Package: p, Digest: hex.EncodeToString(h[:]), Cache: dir, Previous: "previous", Installed: "installed", Args: []string{"-data-dir", "unchanged"}}
			started := ""
			err := apply(j, func() error { return nil }, func(string) (bool, error) {
				if fail {
					return false, errors.New("cancelled")
				}
				return false, nil
			}, func(exe string, args []string) error {
				started = exe
				if len(args) != 2 || args[1] != "unchanged" {
					t.Fatal("lost args")
				}
				return nil
			})
			if (err != nil) != fail {
				t.Fatal(err)
			}
			want := "installed"
			if fail {
				want = "previous"
			}
			if started != want {
				t.Fatalf("started %s", started)
			}
			var r InstallResult
			b, _ := os.ReadFile(filepath.Join(dir, "last-result.json"))
			if e := json.Unmarshal(b, &r); e != nil || r.Success == fail {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
func TestHelperNeverInstallsBeforeParentExits(t *testing.T) {
	called := false
	if e := apply(Job{Cache: t.TempDir()}, func() error { return errors.New("still running") }, func(string) (bool, error) { called = true; return false, nil }, func(string, []string) error { called = true; return nil }); e == nil || called {
		t.Fatal("installed while parent running")
	}
}

func TestHelperRestartFailureKeepsActionableResult(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "update.msi")
	data := []byte("installer")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	j := Job{Package: p, Digest: hex.EncodeToString(h[:]), Cache: dir, Installed: "installed"}
	err := apply(j, func() error { return nil }, func(string) (bool, error) { return false, nil }, func(string, []string) error { return errors.New("application control blocked") })
	if err == nil {
		t.Fatal("restart failure was hidden")
	}
	b, err := os.ReadFile(filepath.Join(dir, "last-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result InstallResult
	if err = json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if result.Success || !strings.Contains(result.Message, "安装完成，但自动启动失败") {
		t.Fatalf("unhelpful result: %+v", result)
	}
}

func TestHelperRestartsEvenIfResultCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "update.msi")
	data := []byte("installer")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	j := Job{Package: p, Digest: hex.EncodeToString(h[:]), Cache: filepath.Join(dir, "missing"), Installed: "installed"}
	started := false
	err := apply(j, func() error { return nil }, func(string) (bool, error) { return false, nil }, func(exe string, args []string) error { started = exe == j.Installed; return nil })
	if err == nil || !started {
		t.Fatalf("log write failure prevented restart: %v", err)
	}
}
