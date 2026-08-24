package protocol

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"polysync/internal/model"
)

const maxFrameSize = 128 << 20

type Hello struct {
	Type       string `json:"type"`
	Protocol   int    `json:"protocol"`
	ShareID    string `json:"shareId"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	ListenPort int    `json:"listenPort"`
	Manual     bool   `json:"manual"`
	Operation  string `json:"operation,omitempty"`
}

type Challenge struct {
	Type             string `json:"type"`
	Nonce            string `json:"nonce"`
	ServerDeviceID   string `json:"serverDeviceId"`
	ServerDeviceName string `json:"serverDeviceName"`
}

type SyncRequest struct {
	Type              string        `json:"type"`
	Auth              string        `json:"auth"`
	Manifest          []model.Entry `json:"manifest"`
	ResolvedConflicts []string      `json:"resolvedConflicts,omitempty"`
}

type PlanMessage struct {
	Type              string     `json:"type"`
	Plan              model.Plan `json:"plan"`
	ResolvedConflicts []string   `json:"resolvedConflicts,omitempty"`
}

type FileHeader struct {
	Type    string `json:"type"`
	Source  string `json:"source"`
	Dest    string `json:"dest"`
	Size    int64  `json:"size"`
	Hash    string `json:"hash"`
	Mode    uint32 `json:"mode"`
	ModTime int64  `json:"modTime"`
}

type Marker struct {
	Type string `json:"type"`
}

type Ack struct {
	Type     string        `json:"type"`
	Manifest []model.Entry `json:"manifest"`
}

type Result struct {
	Type      string `json:"type"`
	OK        bool   `json:"ok"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Sent      int    `json:"sent,omitempty"`
	Received  int    `json:"received,omitempty"`
	Conflicts int    `json:"conflicts,omitempty"`
}

type PairRequest struct {
	Type        string `json:"type"`
	RequestID   string `json:"requestId"`
	ClientNonce string `json:"clientNonce"`
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
	PublicKey   string `json:"publicKey"`
}

type PairChallenge struct {
	Type        string `json:"type"`
	RequestID   string `json:"requestId"`
	ServerNonce string `json:"serverNonce"`
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
	PublicKey   string `json:"publicKey"`
	ExpiresAt   int64  `json:"expiresAt"`
}

type PairConfirm struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Code      string `json:"code"`
	Signature string `json:"signature"`
}

type PairResult struct {
	Type       string `json:"type"`
	OK         bool   `json:"ok"`
	Message    string `json:"message,omitempty"`
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	PublicKey  string `json:"publicKey,omitempty"`
	Signature  string `json:"signature,omitempty"`
}

type ShareInvite struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	ShareID string `json:"shareId"`
	Name    string `json:"name"`
}

type ShareAccept struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	ShareID string `json:"shareId"`
}

type Framer struct {
	reader *bufio.Reader
	writer *bufio.Writer
}

func NewFramer(stream io.ReadWriter) *Framer {
	return &Framer{reader: bufio.NewReaderSize(stream, 256*1024), writer: bufio.NewWriterSize(stream, 256*1024)}
}

func (f *Framer) WriteJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxFrameSize {
		return fmt.Errorf("protocol frame exceeds %d bytes", maxFrameSize)
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	if _, err := f.writer.Write(size[:]); err != nil {
		return err
	}
	if _, err := f.writer.Write(data); err != nil {
		return err
	}
	return f.writer.Flush()
}

func (f *Framer) ReadJSON(target any) error {
	var size [4]byte
	if _, err := io.ReadFull(f.reader, size[:]); err != nil {
		return err
	}
	length := binary.BigEndian.Uint32(size[:])
	if length == 0 || length > maxFrameSize {
		return fmt.Errorf("invalid protocol frame size: %d", length)
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(f.reader, data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode protocol frame: %w", err)
	}
	return nil
}

func (f *Framer) CopyFrom(reader io.Reader, size int64) error {
	if size < 0 {
		return errors.New("negative payload size")
	}
	if _, err := io.CopyN(f.writer, reader, size); err != nil {
		return err
	}
	return f.writer.Flush()
}

func (f *Framer) CopyTo(writer io.Writer, size int64) error {
	if size < 0 {
		return errors.New("negative payload size")
	}
	_, err := io.CopyN(writer, f.reader, size)
	return err
}
