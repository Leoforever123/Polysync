package model

import "time"

const ProtocolVersion = 1

type Config struct {
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	ListenAddr string    `json:"listenAddr"`
	UIAddr     string    `json:"uiAddr"`
	Shares     []Share   `json:"shares"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Share struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	Secret          string `json:"secret"`
	PeerAddress     string `json:"peerAddress,omitempty"`
	AutoSync        bool   `json:"autoSync"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

type Entry struct {
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	Mode    uint32 `json:"mode"`
}

type Transfer struct {
	Source  string `json:"source"`
	Dest    string `json:"dest"`
	Size    int64  `json:"size"`
	Hash    string `json:"hash"`
	Mode    uint32 `json:"mode"`
	ModTime int64  `json:"modTime"`
}

type Plan struct {
	ClientSends   []Transfer `json:"clientSends"`
	ServerSends   []Transfer `json:"serverSends"`
	ClientDeletes []string   `json:"clientDeletes"`
	ServerDeletes []string   `json:"serverDeletes"`
	Conflicts     []string   `json:"conflicts"`
}

type Baseline struct {
	PeerID  string    `json:"peerId"`
	Entries []Entry   `json:"entries"`
	SavedAt time.Time `json:"savedAt"`
}

type RuntimeStatus struct {
	ShareID       string    `json:"shareId"`
	State         string    `json:"state"`
	Message       string    `json:"message,omitempty"`
	LastSync      time.Time `json:"lastSync,omitempty"`
	LastAttempt   time.Time `json:"lastAttempt,omitempty"`
	FilesSent     int       `json:"filesSent"`
	FilesReceived int       `json:"filesReceived"`
	Conflicts     int       `json:"conflicts"`
	PeerName      string    `json:"peerName,omitempty"`
}

type Activity struct {
	Time    time.Time `json:"time"`
	ShareID string    `json:"shareId,omitempty"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type PairCode struct {
	Version int    `json:"version"`
	ShareID string `json:"shareId"`
	Secret  string `json:"secret"`
	Name    string `json:"name"`
}
