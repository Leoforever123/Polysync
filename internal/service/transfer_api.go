package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (s *Service) transferRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/transfers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"tasks": s.transfers.List(), "settings": s.transfers.Settings(), "maxFiles": transferMaxFiles, "maxBytes": transferMaxBytes})
	})
	mux.HandleFunc("PUT /api/transfer-settings", func(w http.ResponseWriter, r *http.Request) {
		var input TransferSettings
		if err := readJSON(r, &input); err != nil {
			writeAPIError(w, 400, err)
			return
		}
		if err := s.SetTransferSettings(input); err != nil {
			writeAPIError(w, 400, err)
			return
		}
		writeJSON(w, 200, s.transfers.Settings())
	})
	mux.HandleFunc("POST /api/transfers", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PeerID string         `json:"peerId"`
			Files  []TransferFile `json:"files"`
		}
		if err := readJSON(r, &input); err != nil {
			writeAPIError(w, 400, err)
			return
		}
		task, err := s.CreateTransfer(input.PeerID, input.Files)
		if err != nil {
			writeAPIError(w, 400, err)
			return
		}
		writeJSON(w, 201, task)
	})
	mux.HandleFunc("PUT /api/transfers/{id}/upload", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, transferMaxBytes+(2<<20))
		defer r.Body.Close()
		reader, err := r.MultipartReader()
		if err != nil {
			writeAPIError(w, 400, err)
			return
		}
		// Cancelling the service or task must also unblock a stalled browser upload.
		taskCtx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stop := context.AfterFunc(s.ctx, func() { cancel(); _ = http.NewResponseController(w).SetReadDeadline(time.Now()); _ = r.Body.Close() })
		defer stop()
		id := r.PathValue("id")
		// UploadTransfer checks cancellation before each write. A read deadline bounds
		// stalled bodies even on clients that stop producing bytes.
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(2 * time.Hour))
		err = s.UploadTransfer(taskCtx, id, reader, transferUploadCloser{r.Body, http.NewResponseController(w)})
		if err != nil {
			writeAPIError(w, 400, err)
			return
		}
		writeJSON(w, 202, map[string]any{"id": id})
	})
	mux.HandleFunc("POST /api/transfers/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if err := s.transfers.Cancel(r.PathValue("id")); err != nil {
			writeAPIError(w, 409, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/transfers/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		task, err := s.RetryTransfer(r.PathValue("id"))
		if err != nil {
			writeAPIError(w, 400, err)
			return
		}
		writeJSON(w, 202, task)
	})
	mux.HandleFunc("DELETE /api/transfers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.transfers.Remove(r.PathValue("id"), r.URL.Query().Get("files") == "true"); err != nil {
			writeAPIError(w, 409, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/transfers/{id}/files/{index}/save", func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(r.PathValue("index"))
		if err != nil {
			writeAPIError(w, 400, err)
			return
		}
		result, err := s.SaveTransferFile(r.PathValue("id"), index)
		if err != nil {
			writeAPIError(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]any{"path": result, "cancelled": result == ""})
	})
	mux.HandleFunc("POST /api/transfers/{id}/files/{index}/open", func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(r.PathValue("index"))
		if err != nil {
			writeAPIError(w, 400, err)
			return
		}
		if err = s.OpenTransferFile(r.PathValue("id"), index); err != nil {
			writeAPIError(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
func (s *Service) SaveTransferFile(id string, index int) (string, error) {
	ctx, done, err := s.beginTransferWork(s.ctx)
	if err != nil {
		return "", err
	}
	defer done()
	task, err := s.transfers.Task(id)
	if err != nil {
		return "", err
	}
	if index < 0 || index >= len(task.Files) || task.Direction != "receive" || task.Files[index].State != "complete" {
		return "", errors.New("文件尚未接收完成")
	}
	f, err := transferFileOpen(task, index)
	if err != nil {
		return "", errors.New("暂存文件已不存在")
	}
	f.Close()
	path, err := chooseTransferSave(ctx, task.Files[index].Name)
	if err != nil || path == "" {
		return "", err
	}
	if err = s.transfers.saveCopyContext(ctx, id, index, path, true); err != nil {
		return "", err
	}
	return path, nil
}
func (s *Service) OpenTransferFile(id string, index int) error {
	task, err := s.transfers.Task(id)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(task.Files) || task.Files[index].State != "complete" {
		return errors.New("文件尚未完成")
	}
	f, err := transferFileOpen(task, index)
	if err != nil {
		return errors.New("暂存文件已不存在")
	}
	f.Close()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", task.StorageDir)
	case "darwin":
		cmd = exec.Command("open", task.StorageDir)
	default:
		cmd = exec.Command("xdg-open", task.StorageDir)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
func chooseTransferSave(ctx context.Context, name string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms; [Console]::OutputEncoding=[Text.Encoding]::UTF8; $d=[System.Windows.Forms.SaveFileDialog]::new(); $d.Title='另存收到的文件'; $d.FileName=$env:POLYSYNC_SAVE_NAME; $d.InitialDirectory=[Environment]::GetFolderPath('MyDocuments'); $d.OverwritePrompt=$true; if($d.ShowDialog() -eq 'OK'){Write-Output $d.FileName}; $d.Dispose()`
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-Command", script)
		cmd.Env = append(os.Environ(), "POLYSYNC_SAVE_NAME="+name)
		hideTransferHost(cmd)
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript", "-e", `on run argv
try
return POSIX path of (choose file name with prompt "另存收到的文件" default name (item 1 of argv))
on error number -128
return ""
end try
end run`, name)
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.CommandContext(ctx, "zenity", "--file-selection", "--save", "--confirm-overwrite", "--filename="+name)
		} else if _, err := exec.LookPath("kdialog"); err == nil {
			cmd = exec.CommandContext(ctx, "kdialog", "--getsavefilename", name)
		} else {
			return "", errors.New("请安装 zenity 或 kdialog 以使用另存对话框")
		}
	}
	output, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if runtime.GOOS == "linux" && errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("无法打开保存对话框: %w", err)
	}
	path := strings.TrimSpace(string(output))
	if path != "" && !filepath.IsAbs(path) {
		return "", errors.New("保存对话框没有返回绝对路径")
	}
	return path, nil
}

// net/http Body.Close alone can wait behind an in-flight Read. Expiring the
// socket read deadline first makes cancellation work for stalled real clients.
type transferUploadCloser struct {
	io.Closer
	controller *http.ResponseController
}

func (b transferUploadCloser) Close() error {
	_ = b.controller.SetReadDeadline(time.Now())
	return b.Closer.Close()
}
