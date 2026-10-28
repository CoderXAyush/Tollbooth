package auth

import (
	"testing"
	"time"
)

func TestAllowAttemptRateLimit(t *testing.T) {
	h := NewSessionHandler(nil)

	for i := 1; i <= 10; i++ {
		if !h.allowAttempt("192.0.2.1") {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}

	if h.allowAttempt("192.0.2.1") {
		t.Fatal("11th attempt should be rate limited")
	}

	// Rate limiting should be independent for each IP.
	if !h.allowAttempt("192.0.2.2") {
		t.Fatal("first attempt from a different IP should be allowed")
	}
}

func TestAllowAttemptResetsAfterWindow(t *testing.T) {
	h := NewSessionHandler(nil)

	now := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }

	for i := 0; i < 10; i++ {
		if !h.allowAttempt("192.0.2.1") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}

	if h.allowAttempt("192.0.2.1") {
		t.Fatal("attempt should be blocked before the window expires")
	}

	now = now.Add(time.Minute)

	if !h.allowAttempt("192.0.2.1") {
		t.Fatal("attempt should be allowed after the window expires")
	}
}

func TestAllowAttemptCleansExpiredEntries(t *testing.T) {
	h := NewSessionHandler(nil)

	now := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }

	if !h.allowAttempt("192.0.2.1") {
		t.Fatal("initial attempt should be allowed")
	}

	now = now.Add(time.Minute)

	if !h.allowAttempt("192.0.2.2") {
		t.Fatal("attempt from a new IP should be allowed")
	}

	if len(h.attempts) != 1 {
		t.Fatalf("expected 1 active entry after cleanup, got %d", len(h.attempts))
	}

	if _, exists := h.attempts["192.0.2.1"]; exists {
		t.Fatal("expected expired IP entry to be removed")
	}
}
