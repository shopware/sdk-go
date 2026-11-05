package auth

import (
	"errors"
	"testing"
	"time"
)

const testSecret = "test-secret-that-is-at-least-32-bytes-long"

func newTestManager(t *testing.T) *TokenManager {
	t.Helper()

	tm, err := NewTokenManager(testSecret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return tm
}

// issueAt mints a token as if the session had started at sessionStart.
func issueAt(t *testing.T, tm *TokenManager, sessionStart time.Time, ttl time.Duration) string {
	t.Helper()

	token, err := tm.seal(ShopContext{
		ShopID:           "shop-1",
		ShopURL:          "https://shop.example.com",
		ExpiresAt:        sessionStart.Add(ttl),
		SessionStartedAt: sessionStart,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return token
}

func TestGenerateAndValidate(t *testing.T) {
	tm := newTestManager(t)

	token, err := tm.Generate("shop-1", "https://shop.example.com", time.Minute)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ctx, err := tm.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if ctx.ShopID != "shop-1" {
		t.Errorf("ShopID = %q, want shop-1", ctx.ShopID)
	}
	if ctx.SessionStartedAt.IsZero() {
		t.Error("SessionStartedAt must be set so the session can be capped")
	}
}

func TestValidateRejectsExpiredToken(t *testing.T) {
	tm := newTestManager(t)
	token := issueAt(t, tm, time.Now().Add(-time.Hour), time.Minute)

	if _, err := tm.Validate(token); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Validate error = %v, want ErrExpiredToken", err)
	}
}

func TestRenewKeepsSessionStart(t *testing.T) {
	tm := newTestManager(t)
	sessionStart := time.Now().Add(-time.Hour)
	token := issueAt(t, tm, sessionStart, 2*time.Hour)

	renewed, ctx, err := tm.Renew(token, RenewOptions{
		Duration:      15 * time.Minute,
		Grace:         5 * time.Minute,
		MaxSessionAge: 8 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if renewed == token {
		t.Error("Renew must issue a new token")
	}

	// A sliding session that forgot its start could be extended forever.
	if !ctx.SessionStartedAt.Equal(sessionStart) {
		t.Errorf("SessionStartedAt = %v, want %v", ctx.SessionStartedAt, sessionStart)
	}

	if _, validateErr := tm.Validate(renewed); validateErr != nil {
		t.Fatalf("renewed token must validate: %v", validateErr)
	}
}

func TestRenewWithinGraceAfterExpiry(t *testing.T) {
	tm := newTestManager(t)
	// Expired two minutes ago, session started ten minutes ago.
	token := issueAt(t, tm, time.Now().Add(-10*time.Minute), 8*time.Minute)

	if _, _, err := tm.Renew(token, RenewOptions{
		Duration:      15 * time.Minute,
		Grace:         5 * time.Minute,
		MaxSessionAge: 8 * time.Hour,
	}); err != nil {
		t.Fatalf("Renew inside grace: %v", err)
	}
}

func TestRenewRejectsBeyondGrace(t *testing.T) {
	tm := newTestManager(t)
	// Expired 20 minutes ago, well outside a 5-minute grace.
	token := issueAt(t, tm, time.Now().Add(-30*time.Minute), 10*time.Minute)

	_, _, err := tm.Renew(token, RenewOptions{
		Duration:      15 * time.Minute,
		Grace:         5 * time.Minute,
		MaxSessionAge: 8 * time.Hour,
	})
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Renew error = %v, want ErrExpiredToken", err)
	}
}

func TestRenewRejectsBeyondSessionCap(t *testing.T) {
	tm := newTestManager(t)
	// Still valid as an access token, but the session is 9 hours old.
	token := issueAt(t, tm, time.Now().Add(-9*time.Hour), 10*time.Hour)

	_, _, err := tm.Renew(token, RenewOptions{
		Duration:      15 * time.Minute,
		Grace:         5 * time.Minute,
		MaxSessionAge: 8 * time.Hour,
	})
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Renew error = %v, want ErrSessionExpired", err)
	}
}

func TestRenewNeverOutlivesSession(t *testing.T) {
	tm := newTestManager(t)
	// Session ends in 5 minutes, but the requested TTL is 15.
	token := issueAt(t, tm, time.Now().Add(-55*time.Minute), time.Hour)

	_, ctx, err := tm.Renew(token, RenewOptions{
		Duration:      15 * time.Minute,
		Grace:         5 * time.Minute,
		MaxSessionAge: time.Hour,
	})
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}

	if remaining := time.Until(ctx.ExpiresAt); remaining > 6*time.Minute {
		t.Errorf("renewed token lives %v, must be clamped to the session end", remaining)
	}
}

func TestRenewRejectsTokenWithoutSession(t *testing.T) {
	tm := newTestManager(t)

	// A token minted before sessions were tracked: its start is unknown, so it
	// must not be renewable.
	token, err := tm.seal(ShopContext{
		ShopID:    "shop-1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, _, renewErr := tm.Renew(token, RenewOptions{
		Duration:      15 * time.Minute,
		MaxSessionAge: 8 * time.Hour,
	}); !errors.Is(renewErr, ErrSessionExpired) {
		t.Fatalf("Renew error = %v, want ErrSessionExpired", renewErr)
	}
}

func TestRenewRejectsGarbage(t *testing.T) {
	tm := newTestManager(t)

	if _, _, err := tm.Renew("not-a-token", RenewOptions{Duration: time.Minute}); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Renew error = %v, want ErrInvalidToken", err)
	}
}
