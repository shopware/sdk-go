package webhook_test

import (
	"bytes"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/webhook"
)

func newBodyRequest(body []byte) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/app/webhook", bytes.NewReader(body))
}

func TestReadLimitedBody_WithinLimit(t *testing.T) {
	body := []byte("hello world")
	req := newBodyRequest(body)

	got, err := webhook.ReadLimitedBody(req, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("expected %q, got %q", body, got)
	}
}

func TestReadLimitedBody_ExactlyAtLimit(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 32)
	req := newBodyRequest(body)

	got, err := webhook.ReadLimitedBody(req, 32)
	if err != nil {
		t.Fatalf("body exactly at limit must be accepted, got error: %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(got))
	}
}

func TestReadLimitedBody_OverLimit(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 33)
	req := newBodyRequest(body)

	_, err := webhook.ReadLimitedBody(req, 32)
	if err == nil {
		t.Fatal("expected error for over-limit body, got nil")
	}
	if !errors.Is(err, sdkerrors.ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}

	var sdkErr *sdkerrors.Error
	if !errors.As(err, &sdkErr) {
		t.Fatalf("expected *sdkerrors.Error, got %T", err)
	}
	if sdkErr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected code 413, got %d", sdkErr.Code)
	}
}

func TestReadLimitedBody_ZeroUsesDefault(t *testing.T) {
	// A body larger than the default should be rejected when maxBytes <= 0.
	body := bytes.Repeat([]byte("a"), int(webhook.DefaultMaxBodyBytes)+1)
	req := newBodyRequest(body)

	_, err := webhook.ReadLimitedBody(req, 0)
	if !errors.Is(err, sdkerrors.ErrPayloadTooLarge) {
		t.Fatalf("expected default limit to reject oversized body, got %v", err)
	}

	// A small body must pass with the default limit.
	small := newBodyRequest([]byte("ok"))
	if _, smallErr := webhook.ReadLimitedBody(small, 0); smallErr != nil {
		t.Fatalf("small body should pass with default limit, got %v", smallErr)
	}
}

func TestReadLimitedBody_LargeLimitBoundary(t *testing.T) {
	// maxBytes+1 overflows to a negative limit ONLY at math.MaxInt64 (it wraps to
	// math.MinInt64). Without the overflow guard, io.LimitReader gets a negative
	// limit, reads 0 bytes, and the body is silently truncated to empty. Both the
	// overflow value and the value just below it must return the full body.
	tests := []struct {
		name     string
		maxBytes int64
	}{
		{"max int64 (arithmetic overflow, guard active)", math.MaxInt64},
		{"just below max int64 (no overflow)", math.MaxInt64 - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte("hello world")
			req := newBodyRequest(body)

			got, err := webhook.ReadLimitedBody(req, tt.maxBytes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("expected full body %q, got %q", body, got)
			}
		})
	}
}

func TestReadLimitedBody_RestoresBody(t *testing.T) {
	body := []byte(`{"source":{"shopId":"abc"}}`)
	req := newBodyRequest(body)

	if _, err := webhook.ReadLimitedBody(req, 1024); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The body must remain readable by downstream handlers.
	restored, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to re-read restored body: %v", err)
	}
	if !bytes.Equal(restored, body) {
		t.Fatalf("restored body mismatch: expected %q, got %q", body, restored)
	}
}

func TestReadLimitedBody_NilBody(t *testing.T) {
	req := &http.Request{}
	if _, err := webhook.ReadLimitedBody(req, 1024); err == nil {
		t.Fatal("expected error for nil body, got nil")
	}
}
