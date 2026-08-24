package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"time"

	"polysync/internal/model"
)

func (s *Service) identityCertificate() (tls.Certificate, error) {
	publicKey, privateKey, err := s.store.Identity()
	if err != nil {
		return tls.Certificate{}, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, err
	}
	config := s.store.Config()
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: config.DeviceID},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey, Leaf: certificate}, nil
}

func (s *Service) serverTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{s.certificate}, MinVersion: tls.VersionTLS13,
		ClientAuth: tls.RequestClientCert,
	}
}

func (s *Service) dialTLS(ctx context.Context, address string, expected *model.PairedDevice) (*tls.Conn, error) {
	config := &tls.Config{
		Certificates: []tls.Certificate{s.certificate}, MinVersion: tls.VersionTLS13,
		InsecureSkipVerify: true,
	}
	if expected != nil {
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("peer did not provide an identity certificate")
			}
			publicKey, err := certificatePublicKey(state.PeerCertificates[0])
			if err != nil {
				return err
			}
			if publicKey != expected.PublicKey {
				return errors.New("peer identity does not match paired device")
			}
			return nil
		}
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}, Config: config}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return connection.(*tls.Conn), nil
}

func certificatePublicKey(certificate *x509.Certificate) (string, error) {
	publicKey, ok := certificate.PublicKey.(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return "", errors.New("peer certificate does not use Ed25519")
	}
	return base64.RawStdEncoding.EncodeToString(publicKey), nil
}

func peerIdentity(connection *tls.Conn) (string, error) {
	state := connection.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return "", errors.New("peer did not provide a certificate")
	}
	return certificatePublicKey(state.PeerCertificates[0])
}

func pairingTranscript(clientKey, serverKey, clientNonce, serverNonce, code string) []byte {
	return []byte("polysync-pair-v2\x00" + clientKey + "\x00" + serverKey + "\x00" + clientNonce + "\x00" + serverNonce + "\x00" + code)
}

func randomPairCode() string {
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", value.Int64())
}

func randomToken(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

func signPairing(privateKey ed25519.PrivateKey, transcript []byte, role string) string {
	payload := append(append([]byte(nil), transcript...), []byte("\x00"+role)...)
	return base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
}

func verifyPairing(publicKeyText, signatureText string, transcript []byte, role string) bool {
	publicKey, err := base64.RawStdEncoding.DecodeString(publicKeyText)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	signature, err := base64.RawStdEncoding.DecodeString(signatureText)
	if err != nil {
		return false
	}
	payload := append(append([]byte(nil), transcript...), []byte("\x00"+role)...)
	return ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature)
}

func remoteAddress(remote net.Addr, port int) string {
	address, ok := remote.(*net.TCPAddr)
	if !ok || port < 1 || port > 65535 {
		return ""
	}
	return net.JoinHostPort(address.IP.String(), strconv.Itoa(port))
}
