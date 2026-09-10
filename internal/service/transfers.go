package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
)

type transferOffer struct {
	Type  string         `json:"type"`
	Files []TransferFile `json:"files"`
}

func (s *Service) beginTransferWork(ctx context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}
	s.wg.Add(1)
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel(); s.wg.Done() }, nil
}
func (s *Service) CreateTransfer(peerID string, files []TransferFile) (TransferTask, error) {
	peer, ok := s.store.PairedDevice(peerID)
	if !ok {
		return TransferTask{}, errors.New("请先配对目标设备")
	}
	if s.ctx.Err() != nil {
		return TransferTask{}, s.ctx.Err()
	}
	// Never accept hashes, blob names or state from the browser.
	for i := range files {
		files[i].Hash = ""
		files[i].Blob = ""
		files[i].State = ""
	}
	task, err := s.transfers.create(peer.ID, peer.Name, "send", files)
	if err == nil {
		time.AfterFunc(2*time.Minute, func() {
			s.transfers.mu.Lock()
			t := s.transfers.tasks[task.ID]
			idle := t != nil && t.State == "preparing" && s.transfers.cancels[task.ID] == nil
			s.transfers.mu.Unlock()
			if idle {
				_ = s.transfers.Cancel(task.ID)
			}
		})
	}
	return task, err
}

type transferWriter struct {
	ctx     context.Context
	writer  io.Writer
	manager *transferManager
	id      string
}

func (w transferWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(p)
	w.manager.progress(w.id, n)
	return n, err
}

type transferReader struct {
	ctx     context.Context
	reader  io.Reader
	manager *transferManager
	id      string
}

func (r transferReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	r.manager.progress(r.id, n)
	return n, err
}

// Upload stages browser-selected files using bounded memory, then dispatches
// a background TLS transfer that survives the browser closing after upload.
func (s *Service) UploadTransfer(ctx context.Context, id string, reader *multipart.Reader, body io.Closer) (err error) {
	task, err := s.transfers.Task(id)
	if err != nil {
		return err
	}
	if task.Direction != "send" || task.State != "preparing" {
		return errors.New("任务不能上传文件")
	}
	ctx, done, err := s.beginTransferWork(ctx)
	if err != nil {
		return err
	}
	defer done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopBody := context.AfterFunc(ctx, func() {
		if body != nil {
			_ = body.Close()
		}
	})
	defer stopBody()
	if err = s.transfers.bind(id, cancel); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			s.transfers.finish(id, err)
		}
	}()
	root, err := os.OpenRoot(task.StorageDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for i, file := range task.Files {
		part, e := reader.NextPart()
		if e != nil {
			return errors.New("上传文件数量不足")
		}
		if part.FormName() != "file" || part.FileName() != file.Name {
			part.Close()
			return errors.New("上传文件与清单不一致")
		}
		f, e := root.OpenFile(file.Blob+".part", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		hash := sha256.New()
		n, e := io.Copy(transferWriter{ctx, io.MultiWriter(f, hash), s.transfers, id}, io.LimitReader(part, file.Size+1))
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if n != file.Size {
			return errors.New("上传文件大小发生变化")
		}
		if e = part.Close(); e != nil {
			return e
		}
		if e = root.Rename(file.Blob+".part", file.Blob); e != nil {
			return e
		}
		if e = s.transfers.fileDone(id, i, hex.EncodeToString(hash.Sum(nil)), "ready"); e != nil {
			return e
		}
	}
	if extra, e := reader.NextPart(); e != io.EOF {
		if extra != nil {
			extra.Close()
		}
		return errors.New("上传文件数量超出清单")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.startSendTransfer(id, ctx)
}
func (s *Service) startSendTransfer(id string, uploadCtx context.Context) error {
	ctx, done, err := s.beginTransferWork(s.ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	m := s.transfers
	m.mu.Lock()
	task := m.tasks[id]
	if err = uploadCtx.Err(); err == nil && (task == nil || task.State != "preparing") {
		err = errors.New("任务已经结束")
	}
	if err == nil {
		task.Done = 0
		task.State = "connecting"
		task.Message = "正在连接设备"
		task.Updated = time.Now()
		err = m.persistLocked(task)
	}
	if err == nil {
		// Replace cancellation under the same lock used by Cancel: a cancelled
		// upload must never be resurrected as a background sender.
		m.cancels[id] = cancel
	}
	m.mu.Unlock()
	if err != nil {
		cancel()
		done()
		return err
	}
	go func() {
		defer done()
		defer cancel()
		err := s.sendTransfer(ctx, id)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		s.transfers.finish(id, err)
	}()
	return nil
}
func (s *Service) sendTransfer(ctx context.Context, id string) error {
	task, err := s.transfers.Task(id)
	if err != nil {
		return err
	}
	peer, ok := s.store.PairedDevice(task.PeerID)
	if !ok {
		return errors.New("目标设备不再处于配对状态")
	}
	connection, err := s.dialTLS(ctx, s.bestDeviceAddress(peer), &peer)
	if err != nil {
		return fmt.Errorf("连接目标设备失败: %w", err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Hour))
	framer := protocol.NewFramer(connection)
	config := s.store.Config()
	if err := framer.WriteJSON(protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "transfer", DeviceID: config.DeviceID, DeviceName: config.DeviceName, ListenPort: s.listenPort}); err != nil {
		return err
	}
	// Authenticate and obtain capability/readiness before sending any file metadata.
	var result protocol.Result
	if err := framer.ReadJSON(&result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("对方无法接收随传: %s", result.Message)
	}
	files := make([]TransferFile, len(task.Files))
	for i, f := range task.Files {
		files[i] = TransferFile{Name: f.Name, Size: f.Size, Hash: f.Hash}
	}
	if err := framer.WriteJSON(transferOffer{Type: "transfer_offer", Files: files}); err != nil {
		return err
	}
	if err := framer.ReadJSON(&result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New(result.Message)
	}
	if err := s.transfers.update(id, "sending", "正在发送"); err != nil {
		return err
	}
	for i, file := range task.Files {
		f, err := transferFileOpen(task, i)
		if err != nil {
			return err
		}
		hash := sha256.New()
		err = framer.CopyFrom(transferReader{ctx, io.TeeReader(f, hash), s.transfers, id}, file.Size)
		f.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != file.Hash {
			return errors.New("发送文件已经变化")
		}
		if err = framer.ReadJSON(&result); err != nil {
			return err
		}
		if !result.OK {
			return errors.New(result.Message)
		}
		if err = s.transfers.fileDone(id, i, file.Hash, "complete"); err != nil {
			return err
		}
	}
	if err := s.transfers.update(id, "verifying", "等待对方确认接收结果"); err != nil {
		return err
	}
	if err := framer.ReadJSON(&result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New(result.Message)
	}
	return nil
}
func (s *Service) handleTransfer(ctx context.Context, connection *tls.Conn, framer *protocol.Framer, hello protocol.Hello) {
	peer, err := s.requirePaired(connection, hello.DeviceID)
	if err != nil {
		_ = framer.WriteJSON(protocol.Result{Type: "transfer_ready", Message: err.Error()})
		return
	}
	if !s.transfers.Settings().AutoReceive {
		_ = framer.WriteJSON(protocol.Result{Type: "transfer_ready", Message: "对方已关闭随传接收"})
		return
	}
	if err := framer.WriteJSON(protocol.Result{Type: "transfer_ready", OK: true}); err != nil {
		return
	}
	var offer transferOffer
	if err := framer.ReadJSON(&offer); err != nil {
		return
	}
	if offer.Type != "transfer_offer" {
		_ = framer.WriteJSON(protocol.Result{Message: "传输清单无效"})
		return
	}
	for _, f := range offer.Files {
		hash, e := hex.DecodeString(f.Hash)
		if e != nil || len(hash) != 32 {
			_ = framer.WriteJSON(protocol.Result{Message: "文件校验值无效"})
			return
		}
	}
	s.transferConfigMu.Lock()
	task, err := s.transfers.create(peer.ID, peer.Name, "receive", offer.Files)
	s.transferConfigMu.Unlock()
	if err != nil {
		_ = framer.WriteJSON(protocol.Result{Message: err.Error()})
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err = s.transfers.bind(task.ID, cancel); err != nil {
		s.transfers.finish(task.ID, err)
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	err = s.receiveTransfer(ctx, task, framer)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	s.transfers.finish(task.ID, err)
	// A final acknowledgement follows durable record completion.
	saved, _ := s.transfers.Task(task.ID)
	_ = framer.WriteJSON(protocol.Result{Type: "transfer_result", OK: saved.State == "complete", Message: saved.Message})
}
func (s *Service) receiveTransfer(ctx context.Context, task TransferTask, framer *protocol.Framer) (err error) {
	root, err := os.OpenRoot(task.StorageDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = framer.WriteJSON(protocol.Result{Type: "transfer_accept", OK: true}); err != nil {
		return err
	}
	for i, file := range task.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		f, e := root.OpenFile(file.Blob+".part", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		hash := sha256.New()
		e = framer.CopyTo(transferWriter{ctx, io.MultiWriter(f, hash), s.transfers, task.ID}, file.Size)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if hex.EncodeToString(hash.Sum(nil)) != file.Hash {
			_ = framer.WriteJSON(protocol.Result{Type: "file_result", Message: "文件 SHA-256 校验失败"})
			return errors.New("文件 SHA-256 校验失败")
		}
		if e = root.Rename(file.Blob+".part", file.Blob); e != nil {
			return e
		}
		if e = s.transfers.fileDone(task.ID, i, file.Hash, "complete"); e != nil {
			return e
		}
		if e = framer.WriteJSON(protocol.Result{Type: "file_result", OK: true}); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) RetryTransfer(id string) (TransferTask, error) {
	old, err := s.transfers.Task(id)
	if err != nil {
		return TransferTask{}, err
	}
	if old.Direction != "send" || transferActive(old.State) {
		return TransferTask{}, errors.New("只能重新发送已结束的发送任务")
	}
	for i := range old.Files {
		f, e := transferFileOpen(old, i)
		if e != nil {
			return TransferTask{}, errors.New("发送缓存已不存在，请重新选择文件")
		}
		f.Close()
	}
	fresh, err := s.CreateTransfer(old.PeerID, append([]TransferFile(nil), old.Files...))
	if err != nil {
		return TransferTask{}, err
	}
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	boundary := writer.Boundary()
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		var copyErr error
		for i, file := range old.Files {
			src, e := transferFileOpen(old, i)
			if e != nil {
				copyErr = e
				break
			}
			part, e := writer.CreateFormFile("file", file.Name)
			if e != nil {
				src.Close()
				copyErr = e
				break
			}
			hash := sha256.New()
			n, e := io.Copy(io.MultiWriter(part, hash), src)
			src.Close()
			if e != nil {
				copyErr = e
				break
			}
			if n != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.Hash {
				copyErr = errors.New("发送缓存发生变化，请重新选择文件")
				break
			}
		}
		if copyErr == nil {
			copyErr = writer.Close()
		}
		_ = pw.CloseWithError(copyErr)
	}()
	err = s.UploadTransfer(s.ctx, fresh.ID, multipart.NewReader(pr, boundary), pr)
	pr.Close()
	<-copied
	return fresh, err
}
