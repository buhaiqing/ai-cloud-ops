package ingest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// sign is the test helper that produces the signature VerifySignature expects.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_Valid(t *testing.T) {
	body := []byte(`{"alert_id":"abc"}`)
	secret := "shh"
	if !VerifySignature(body, sign(body, secret), secret) {
		t.Fatal("valid signature must pass")
	}
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	body := []byte(`{"alert_id":"abc"}`)
	sig := sign(body, "shh")
	tampered := []byte(`{"alert_id":"XYZ"}`)
	if VerifySignature(tampered, sig, "shh") {
		t.Fatal("tampered body must fail")
	}
}

func TestVerifySignature_WrongSecret(t *testing.T) {
	body := []byte(`{"alert_id":"abc"}`)
	sig := sign(body, "real-secret")
	if VerifySignature(body, sig, "other-secret") {
		t.Fatal("wrong secret must fail")
	}
}

func TestVerifySignature_EmptySignature(t *testing.T) {
	if VerifySignature([]byte("body"), "", "shh") {
		t.Fatal("empty signature must fail")
	}
}

func TestVerifySignature_EmptySecret(t *testing.T) {
	// Defense in depth: even if signature is non-empty, missing secret refuses.
	if VerifySignature([]byte("body"), "deadbeef", "") {
		t.Fatal("empty secret must fail")
	}
}