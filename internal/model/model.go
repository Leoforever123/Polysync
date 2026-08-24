package model

import "time"

const ProtocolVersion = 2

type Config struct {
	DeviceID           string            `json:"deviceId"`
	DeviceName         string            `json:"deviceName"`
	IdentityPublicKey  string            `json:"identityPublicKey"`
	IdentityPrivateKey string            `json:"identityPrivateKey"`
	ListenAddr         string            `json:"listenAddr"`
	UIAddr             string            `json:"uiAddr"`
	PairedDevices      []PairedDevice    `json:"pairedDevices"`
	ShareInvitations   []ShareInvitation `json:"shareInvitations,omitempty"`
	Shares             []Share           `json:"shares"`
	CreatedAt          time.Time         `json:"createdAt"`
}

type PairedDevice struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"publicKey"`
	Addresses []string  `json:"addresses"`
	PairedAt  time.Time `json:"pairedAt"`
	LastSeen  time.Time `json:"lastSeen,omitempty"`
}

type NearbyDevice struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"publicKey"`
	Fingerprint string    `json:"fingerprint"`
	Addresses   []string  `json:"addresses"`
	LastSeen    time.Time `json:"lastSeen"`
	Paired      bool      `json:"paired"`
	Online      bool      `json:"online"`
}

type Share struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	Secret          string `json:"secret"`
	PeerDeviceID    string `json:"peerDeviceId,omitempty"`
	PeerAddress     string `json:"peerAddress,omitempty"`
	State           string `json:"state,omitempty"`
	AutoSync        bool   `json:"autoSync"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

type PairingRequest struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	PublicKey  string    `json:"publicKey"`
	Address    string    `json:"address"`
	Code       string    `json:"code"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type ShareInvitation struct {
	ID         string    `json:"id"`
	ShareID    string    `json:"shareId"`
	Name       string    `json:"name"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	Address    string    `json:"address"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Conflict struct {
	ID               string    `json:"id"`
	ShareID          string    `json:"shareId"`
	Path             string    `json:"path"`
	Kind             string    `json:"kind"`
	BaseHash         string    `json:"baseHash,omitempty"`
	LocalHash        string    `json:"localHash,omitempty"`
	RemoteHash       string    `json:"remoteHash,omitempty"`
	LocalExists      bool      `json:"localExists"`
	RemoteExists     bool      `json:"remoteExists"`
	LocalDevice      string    `json:"localDevice"`
	RemoteDevice     string    `json:"remoteDevice"`
	ConflictCopyPath string    `json:"conflictCopyPath,omitempty"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
	ResolvedAt       time.Time `json:"resolvedAt,omitempty"`
}

type PlanConflict struct {
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	BaseHash         string `json:"baseHash,omitempty"`
	ServerHash       string `json:"serverHash,omitempty"`
	ClientHash       string `json:"clientHash,omitempty"`
	ServerExists     bool   `json:"serverExists"`
	ClientExists     bool   `json:"clientExists"`
	ConflictCopyPath string `json:"conflictCopyPath,omitempty"`
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
	ClientSends     []Transfer     `json:"clientSends"`
	ServerSends     []Transfer     `json:"serverSends"`
	ClientDeletes   []string       `json:"clientDeletes"`
	ServerDeletes   []string       `json:"serverDeletes"`
	Conflicts       []string       `json:"conflicts"`
	ConflictDetails []PlanConflict `json:"conflictDetails,omitempty"`
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
