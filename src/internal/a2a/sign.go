package a2a

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

// Feishu-style request signing: a peer that shares the secret signs each
// request with HMAC-SHA256 so the receiver can authenticate it and reject
// replays. Mirrors the custom-bot signature check from the Feishu docs:
//
//	string_to_sign = timestamp + "\n" + secret
//	signature      = base64(HMAC-SHA256(key=string_to_sign, msg=""))
//
// Headers carried on every signed JSON-RPC request:
//
//	X-A2A-Timestamp: <unix seconds>
//	X-A2A-Signature: <signature>
const (
	HeaderTimestamp = "X-A2A-Timestamp"
	HeaderSignature = "X-A2A-Signature"
	signMaxSkew     = 5 * time.Minute
)

// Sign returns (timestamp, signature) for the given secret at now.
func Sign(secret string, now time.Time) (timestamp, signature string) {
	timestamp = strconv.FormatInt(now.Unix(), 10)
	h := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	signature = base64.StdEncoding.EncodeToString(h.Sum(nil))
	return timestamp, signature
}

// VerifySignature validates a (timestamp, signature) pair against the secret.
// An empty secret disables verification (backward-compatible: a server that
// has not configured a secret does not require clients to sign).
func VerifySignature(secret, timestamp, signature string, now time.Time) error {
	if secret == "" {
		return nil
	}
	if timestamp == "" || signature == "" {
		return fmt.Errorf("a2a: missing signature headers")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("a2a: invalid timestamp: %w", err)
	}
	if diff := now.Sub(time.Unix(ts, 0)); diff > signMaxSkew || diff < -signMaxSkew {
		return fmt.Errorf("a2a: timestamp out of allowed skew (%s)", diff.Truncate(time.Second))
	}
	h := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	expected := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("a2a: signature mismatch")
	}
	return nil
}
