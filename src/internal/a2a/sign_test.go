package a2a

import (
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	now := time.Now()
	ts, sig := Sign("s3cret-key", now)
	if err := VerifySignature("s3cret-key", ts, sig, now); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	now := time.Now()
	ts, sig := Sign("secret-a", now)
	if err := VerifySignature("secret-b", ts, sig, now); err == nil {
		t.Fatal("expected signature mismatch error, got nil")
	}
}

func TestVerifyRejectsStaleTimestamp(t *testing.T) {
	now := time.Now()
	ts, sig := Sign("secret", now)
	// 10 minutes later — outside the ±5m skew window (replay protection).
	if err := VerifySignature("secret", ts, sig, now.Add(10*time.Minute)); err == nil {
		t.Fatal("expected timestamp skew error, got nil")
	}
}

func TestVerifyEmptySecretSkipsCheck(t *testing.T) {
	// No secret configured → verification disabled; missing headers are allowed.
	if err := VerifySignature("", "", "", time.Now()); err != nil {
		t.Fatalf("expected nil for empty secret, got %v", err)
	}
}

func TestVerifyRejectsMissingHeaders(t *testing.T) {
	if err := VerifySignature("secret", "", "", time.Now()); err == nil {
		t.Fatal("expected missing-headers error, got nil")
	}
}
