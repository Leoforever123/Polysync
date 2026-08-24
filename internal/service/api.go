package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"polysync/internal/model"
	"polysync/internal/store"
	"polysync/internal/webui"
)

type shareRequest struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	PeerAddress     string `json:"peerAddress"`
	PairCode        string `json:"pairCode"`
	AutoSync        bool   `json:"autoSync"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

type shareView struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Path            string              `json:"path"`
	PeerAddress     string              `json:"peerAddress,omitempty"`
	AutoSync        bool                `json:"autoSync"`
	IntervalSeconds int                 `json:"intervalSeconds"`
	PairCode        string              `json:"pairCode"`
	Status          model.RuntimeStatus `json:"status"`
	PeerDeviceID    string              `json:"peerDeviceId,omitempty"`
	State           string              `json:"state"`
}

type pairedDeviceView struct {
	model.PairedDevice
	Online bool `json:"online"`
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/pair/start", s.handleStartPairing)
	mux.HandleFunc("POST /api/pair/confirm", s.handleConfirmPairing)
	mux.HandleFunc("POST /api/pair/requests/{id}/approve", s.handleApprovePairing)
	mux.HandleFunc("DELETE /api/pair/requests/{id}", s.handleRejectPairing)
	mux.HandleFunc("POST /api/shares/invite", s.handleInviteShare)
	mux.HandleFunc("POST /api/share-invitations/{id}/accept", s.handleAcceptInvitation)
	mux.HandleFunc("GET /api/conflicts/{id}", s.handleConflictContent)
	mux.HandleFunc("POST /api/conflicts/{id}/resolve", s.handleResolveConflict)
	mux.HandleFunc("POST /api/shares", s.handleCreateShare)
	mux.HandleFunc("PUT /api/shares/{id}", s.handleUpdateShare)
	mux.HandleFunc("DELETE /api/shares/{id}", s.handleDeleteShare)
	mux.HandleFunc("POST /api/shares/{id}/sync", s.handleSyncShare)
	mux.HandleFunc("PUT /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/pick-folder", s.handlePickFolder)
	mux.Handle("/", webui.Handler())
	return securityHeaders(mux)
}

func (s *Service) handleStatus(writer http.ResponseWriter, _ *http.Request) {
	config := s.store.Config()
	statuses := s.Statuses()
	nearby := s.NearbyDevices()
	online := make(map[string]bool)
	for _, device := range nearby {
		online[device.ID] = device.Online
	}
	pairedDevices := make([]pairedDeviceView, 0, len(config.PairedDevices))
	for _, device := range config.PairedDevices {
		pairedDevices = append(pairedDevices, pairedDeviceView{PairedDevice: device, Online: online[device.ID] || time.Since(device.LastSeen) < 90*time.Second})
	}
	conflicts, _ := s.store.Conflicts()
	shares := make([]shareView, 0, len(config.Shares))
	for _, share := range config.Shares {
		shares = append(shares, shareView{
			ID: share.ID, Name: share.Name, Path: share.Path, PeerAddress: share.PeerAddress,
			AutoSync: share.AutoSync, IntervalSeconds: share.IntervalSeconds,
			PairCode: encodePairCode(share), Status: statuses[share.ID], PeerDeviceID: share.PeerDeviceID, State: share.State,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"device": map[string]any{
			"id": config.DeviceID, "name": config.DeviceName, "listenPort": s.listenPort,
			"addresses": localAddresses(s.listenPort), "platform": runtime.GOOS,
		},
		"shares": shares, "activities": s.Activities(), "protocolVersion": model.ProtocolVersion,
		"nearbyDevices": nearby, "pairedDevices": pairedDevices, "pairingRequests": s.PairingRequests(),
		"shareInvitations": s.ShareInvitations(), "conflicts": conflicts,
	})
}

func (s *Service) handleStartPairing(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DeviceID string `json:"deviceId"`
		Address  string `json:"address"`
	}
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	sessionID, err := s.StartPairing(request.Context(), input.DeviceID, input.Address)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"sessionId": sessionID})
}

func (s *Service) handleConfirmPairing(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		SessionID string `json:"sessionId"`
		Code      string `json:"code"`
	}
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	if err := s.ConfirmPairing(request.Context(), input.SessionID, strings.TrimSpace(input.Code)); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleApprovePairing(writer http.ResponseWriter, request *http.Request) {
	if err := s.ApprovePairingRequest(request.PathValue("id")); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleRejectPairing(writer http.ResponseWriter, request *http.Request) {
	s.RejectPairingRequest(request.PathValue("id"))
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleInviteShare(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DeviceID        string `json:"deviceId"`
		Name            string `json:"name"`
		Path            string `json:"path"`
		AutoSync        bool   `json:"autoSync"`
		IntervalSeconds int    `json:"intervalSeconds"`
	}
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	path, err := s.preparePath(input.Path, "")
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(path)
	}
	share, err := s.InviteShare(request.Context(), input.DeviceID, name, path, input.AutoSync, normalizedInterval(input.IntervalSeconds))
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"id": share.ID})
}

func (s *Service) handleAcceptInvitation(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Path            string `json:"path"`
		AutoSync        bool   `json:"autoSync"`
		IntervalSeconds int    `json:"intervalSeconds"`
	}
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	path, err := s.preparePath(input.Path, "")
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	share, err := s.AcceptShareInvitation(request.Context(), request.PathValue("id"), path, input.AutoSync, normalizedInterval(input.IntervalSeconds))
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"id": share.ID})
}

func (s *Service) handleConflictContent(writer http.ResponseWriter, request *http.Request) {
	content, err := s.ConflictContent(request.PathValue("id"))
	if err != nil {
		writeAPIError(writer, http.StatusNotFound, err)
		return
	}
	writeJSON(writer, http.StatusOK, content)
}

func (s *Service) handleResolveConflict(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Choice  string `json:"choice"`
		Content string `json:"content"`
	}
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	if err := s.ResolveConflict(request.Context(), request.PathValue("id"), input.Choice, input.Content); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleCreateShare(writer http.ResponseWriter, request *http.Request) {
	var input shareRequest
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	path, err := s.preparePath(input.Path, "")
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	interval := normalizedInterval(input.IntervalSeconds)
	share := model.Share{
		ID: store.RandomID(), Secret: store.RandomSecret(), Name: strings.TrimSpace(input.Name), Path: path,
		PeerAddress: strings.TrimSpace(input.PeerAddress), State: "legacy", AutoSync: input.AutoSync, IntervalSeconds: interval,
	}
	if input.PairCode != "" {
		pair, err := decodePairCode(input.PairCode)
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, err)
			return
		}
		share.ID = pair.ShareID
		share.Secret = pair.Secret
		if share.Name == "" {
			share.Name = pair.Name
		}
		if share.PeerAddress == "" {
			writeAPIError(writer, http.StatusBadRequest, errors.New("加入同步时必须填写对端地址"))
			return
		}
	}
	if share.Name == "" {
		share.Name = filepath.Base(path)
	}
	if err := validatePeerAddress(share.PeerAddress); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	if err := s.store.AddShare(share); err != nil {
		writeAPIError(writer, http.StatusConflict, err)
		return
	}
	s.RegisterShare(share.ID)
	s.log("info", share.ID, "已添加同步文件夹 “"+share.Name+"”")
	writeJSON(writer, http.StatusCreated, map[string]any{"id": share.ID, "pairCode": encodePairCode(share)})
}

func (s *Service) handleUpdateShare(writer http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	share, exists := s.store.Share(id)
	if !exists {
		writeAPIError(writer, http.StatusNotFound, errors.New("同步文件夹不存在"))
		return
	}
	var input shareRequest
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	path, err := s.preparePath(input.Path, id)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	if err := validatePeerAddress(input.PeerAddress); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	share.Name = strings.TrimSpace(input.Name)
	if share.Name == "" {
		share.Name = filepath.Base(path)
	}
	share.Path = path
	share.PeerAddress = strings.TrimSpace(input.PeerAddress)
	share.AutoSync = input.AutoSync
	share.IntervalSeconds = normalizedInterval(input.IntervalSeconds)
	if err := s.store.UpdateShare(share); err != nil {
		writeAPIError(writer, http.StatusInternalServerError, err)
		return
	}
	s.log("info", share.ID, "已更新同步设置")
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleDeleteShare(writer http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	share, exists := s.store.Share(id)
	if !exists {
		writeAPIError(writer, http.StatusNotFound, errors.New("同步文件夹不存在"))
		return
	}
	if err := s.store.DeleteShare(id); err != nil {
		writeAPIError(writer, http.StatusInternalServerError, err)
		return
	}
	s.RemoveShare(id)
	s.log("info", "", "已移除同步配置 “"+share.Name+"”；本地文件未被删除")
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleSyncShare(writer http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	share, exists := s.store.Share(id)
	if !exists {
		writeAPIError(writer, http.StatusNotFound, errors.New("同步文件夹不存在"))
		return
	}
	if share.State != "active" || share.PeerDeviceID == "" {
		writeAPIError(writer, http.StatusBadRequest, errors.New("同步空间尚未被已配对设备接受"))
		return
	}
	go func() { _ = s.SyncShare(context.Background(), id, true) }()
	writeJSON(writer, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Service) handleSettings(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DeviceName string `json:"deviceName"`
	}
	if err := readJSON(request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetDeviceName(input.DeviceName); err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handlePickFolder(writer http.ResponseWriter, _ *http.Request) {
	path, err := pickFolder()
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"path": path})
}

func (s *Service) preparePath(input, exceptID string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("请选择本地文件夹")
	}
	absolute, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return "", fmt.Errorf("无法创建文件夹: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", errors.New("路径不是可访问的文件夹")
	}
	for _, existing := range s.store.Config().Shares {
		if existing.ID == exceptID {
			continue
		}
		if pathsOverlap(absolute, existing.Path) {
			return "", fmt.Errorf("不能与已有同步文件夹 “%s” 重叠", existing.Name)
		}
	}
	return filepath.Clean(absolute), nil
}

func normalizedInterval(value int) int {
	if value < 5 {
		return 30
	}
	if value > 86400 {
		return 86400
	}
	return value
}

func pathsOverlap(left, right string) bool {
	left, _ = filepath.Abs(left)
	right, _ = filepath.Abs(right)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		left, right = strings.ToLower(left), strings.ToLower(right)
	}
	if left == right {
		return true
	}
	separator := string(filepath.Separator)
	return strings.HasPrefix(left, right+separator) || strings.HasPrefix(right, left+separator)
}

func encodePairCode(share model.Share) string {
	data, _ := json.Marshal(model.PairCode{Version: model.ProtocolVersion, ShareID: share.ID, Secret: share.Secret, Name: share.Name})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodePairCode(code string) (model.PairCode, error) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code))
	if err != nil {
		return model.PairCode{}, errors.New("同步码格式无效")
	}
	var pair model.PairCode
	if err := json.Unmarshal(data, &pair); err != nil || pair.Version != model.ProtocolVersion || pair.ShareID == "" || len(pair.Secret) < 32 {
		return model.PairCode{}, errors.New("同步码无效或版本不兼容")
	}
	return pair, nil
}

func validatePeerAddress(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return errors.New("对端地址应为 IP:端口，例如 192.168.1.20:45123")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("对端端口无效")
	}
	return nil
}

func localAddresses(port int) []string {
	addresses := []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		items, _ := iface.Addrs()
		for _, item := range items {
			var ip net.IP
			switch value := item.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			addresses = append(addresses, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		}
	}
	return addresses
}

func pickFolder() (string, error) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; if ($d.ShowDialog() -eq 'OK') { [Console]::OutputEncoding = [Text.Encoding]::UTF8; Write-Output $d.SelectedPath }`
		command = exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", script)
	case "darwin":
		command = exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "选择同步文件夹")`)
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			command = exec.Command("zenity", "--file-selection", "--directory", "--title=选择同步文件夹")
		} else if _, err := exec.LookPath("kdialog"); err == nil {
			command = exec.Command("kdialog", "--getexistingdirectory", ".")
		} else {
			return "", errors.New("未找到系统文件夹选择器，请直接输入绝对路径")
		}
	}
	output, err := command.Output()
	if err != nil {
		return "", errors.New("未选择文件夹")
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", errors.New("未选择文件夹")
	}
	return path, nil
}

func readJSON(request *http.Request, target any) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("请求数据格式无效")
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeAPIError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]any{"error": err.Error()})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		if request.Method != http.MethodGet && request.Method != http.MethodHead && !sameOrigin(request) {
			writeAPIError(writer, http.StatusForbidden, errors.New("拒绝跨站请求"))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, request.Host)
}

var _ = time.Second
