package protocol

import "testing"

func TestAuthentication(t *testing.T) {
	auth := Authentication("secret", "nonce", "share", "device")
	if !VerifyAuthentication(auth, "secret", "nonce", "share", "device") {
		t.Fatal("valid authentication was rejected")
	}
	if VerifyAuthentication(auth, "other", "nonce", "share", "device") {
		t.Fatal("invalid secret was accepted")
	}
}
