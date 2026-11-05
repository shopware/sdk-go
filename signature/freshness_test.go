package signature_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/shopware/sdk-go/signature"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckFreshness(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0)
	at := func(offset time.Duration) string {
		return strconv.FormatInt(now.Add(offset).Unix(), 10)
	}

	tests := []struct {
		name      string
		timestamp string
		maxAge    time.Duration
		skew      time.Duration
		wantErr   error
	}{
		{name: "current timestamp", timestamp: at(0), maxAge: time.Minute, skew: time.Minute},
		{name: "inside window", timestamp: at(-30 * time.Second), maxAge: time.Minute, skew: time.Minute},
		{name: "inside skew", timestamp: at(30 * time.Second), maxAge: time.Minute, skew: time.Minute},
		{
			name:      "empty timestamp",
			timestamp: "",
			maxAge:    time.Minute,
			skew:      time.Minute,
			wantErr:   signature.ErrMissingTimestamp,
		},
		{
			name:      "not a number",
			timestamp: "not-a-number",
			maxAge:    time.Minute,
			skew:      time.Minute,
			wantErr:   signature.ErrInvalidTimestamp,
		},
		{
			name:      "too old",
			timestamp: at(-2 * time.Minute),
			maxAge:    time.Minute,
			skew:      time.Minute,
			wantErr:   signature.ErrTimestampOutsideWindow,
		},
		{
			name:      "too far in the future",
			timestamp: at(2 * time.Minute),
			maxAge:    time.Minute,
			skew:      time.Minute,
			wantErr:   signature.ErrTimestampOutsideWindow,
		},

		// maxAge and skew <= 0 fall back to the SDK defaults, so the accepted
		// window is ReplayWindowSeconds in the past and ClockSkewSeconds ahead.
		{name: "zero maxAge falls back to default window", timestamp: at(-4 * time.Minute), maxAge: 0, skew: time.Minute},
		{
			name:      "zero maxAge still rejects beyond default window",
			timestamp: at(-6 * time.Minute),
			maxAge:    0,
			skew:      time.Minute,
			wantErr:   signature.ErrTimestampOutsideWindow,
		},
		{name: "zero skew falls back to default skew", timestamp: at(30 * time.Second), maxAge: time.Minute, skew: 0},
		{
			name:      "zero skew still rejects beyond default skew",
			timestamp: at(2 * time.Minute),
			maxAge:    time.Minute,
			skew:      0,
			wantErr:   signature.ErrTimestampOutsideWindow,
		},
		{name: "negative maxAge falls back to default window", timestamp: at(-4 * time.Minute), maxAge: -time.Hour, skew: -time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := signature.CheckFreshness(tt.timestamp, tt.maxAge, tt.skew, now)
			if tt.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCheckFreshness_ErrorMessages(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0)

	stale := signature.CheckFreshness(strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10), time.Minute, time.Minute, now)
	require.Error(t, stale)
	assert.Contains(t, stale.Error(), "10m0s old")

	future := signature.CheckFreshness(strconv.FormatInt(now.Add(10*time.Minute).Unix(), 10), time.Minute, time.Minute, now)
	require.Error(t, future)
	assert.Contains(t, future.Error(), "10m0s in the future")

	malformed := signature.CheckFreshness("1.5", time.Minute, time.Minute, now)
	require.Error(t, malformed)
	assert.Contains(t, malformed.Error(), "1.5")
}
