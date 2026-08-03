// Package ingest ingests alerts from upstream sources (T4: CloudMonitor webhook).
//
// signature.go: HMAC-SHA256 verification for Aliyun CloudMonitor EventSubscription
// webhooks. Mirrors ai_cloud_ops.ingest.webhook._verify_signature (Python).
package ingest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// VerifySignature returns true iff signature equals hex(HMAC-SHA256(secret, body)).
// Comparison is constant-time via hmac.Equal to defeat timing oracles.
// Empty signature or empty secret always fails.
func VerifySignature(body []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}