package signature_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/shopware/sdk-go/signature"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreshnessPolicy_WithDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		policy        signature.FreshnessPolicy
		wantMaxAge    time.Duration
		wantClockSkew time.Duration
	}{
		{
			name:          "zero value gets both SDK defaults",
			policy:        signature.FreshnessPolicy{},
			wantMaxAge:    signature.DefaultReplayWindow,
			wantClockSkew: signature.DefaultClockSkew,
		},
		{
			name:          "explicit durations are preserved",
			policy:        signature.FreshnessPolicy{MaxAge: 42 * time.Second, ClockSkew: 7 * time.Second},
			wantMaxAge:    42 * time.Second,
			wantClockSkew: 7 * time.Second,
		},
		{
			name:          "only MaxAge set",
			policy:        signature.FreshnessPolicy{MaxAge: 42 * time.Second},
			wantMaxAge:    42 * time.Second,
			wantClockSkew: signature.DefaultClockSkew,
		},
		{
			name:          "only ClockSkew set",
			policy:        signature.FreshnessPolicy{ClockSkew: 7 * time.Second},
			wantMaxAge:    signature.DefaultReplayWindow,
			wantClockSkew: 7 * time.Second,
		},
		{
			name:          "negative durations fall back to the defaults",
			policy:        signature.FreshnessPolicy{MaxAge: -time.Second, ClockSkew: -time.Second},
			wantMaxAge:    signature.DefaultReplayWindow,
			wantClockSkew: signature.DefaultClockSkew,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			effective := tt.policy.WithDefaults()
			assert.Equal(t, tt.wantMaxAge, effective.MaxAge)
			assert.Equal(t, tt.wantClockSkew, effective.ClockSkew)
			assert.Equal(t, tt.policy.Mode, effective.Mode, "WithDefaults must not touch the mode")
		})
	}
}

func TestFreshnessPolicy_Check(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0)
	at := func(offset time.Duration) string {
		return strconv.FormatInt(now.Add(offset).Unix(), 10)
	}

	tests := []struct {
		name      string
		mode      signature.TimestampMode
		timestamp string
		wantErr   error
	}{
		// TimestampUnset must behave exactly like TimestampRequire: the zero
		// value of the policy fails closed.
		{name: "unset accepts a fresh timestamp", mode: signature.TimestampUnset, timestamp: at(0)},
		{
			name:      "unset rejects a missing timestamp",
			mode:      signature.TimestampUnset,
			timestamp: "",
			wantErr:   signature.ErrMissingTimestamp,
		},
		{
			name:      "unset rejects a stale timestamp",
			mode:      signature.TimestampUnset,
			timestamp: at(-10 * time.Minute),
			wantErr:   signature.ErrTimestampOutsideWindow,
		},

		{name: "require accepts a fresh timestamp", mode: signature.TimestampRequire, timestamp: at(0)},
		{
			name:      "require rejects a missing timestamp",
			mode:      signature.TimestampRequire,
			timestamp: "",
			wantErr:   signature.ErrMissingTimestamp,
		},
		{
			name:      "require rejects a stale timestamp",
			mode:      signature.TimestampRequire,
			timestamp: at(-10 * time.Minute),
			wantErr:   signature.ErrTimestampOutsideWindow,
		},
		{
			name:      "require rejects a malformed timestamp",
			mode:      signature.TimestampRequire,
			timestamp: "not-a-number",
			wantErr:   signature.ErrInvalidTimestamp,
		},

		{name: "verify-if-present accepts a missing timestamp", mode: signature.TimestampVerifyIfPresent, timestamp: ""},
		{name: "verify-if-present accepts a fresh timestamp", mode: signature.TimestampVerifyIfPresent, timestamp: at(0)},
		{
			name:      "verify-if-present rejects a stale timestamp",
			mode:      signature.TimestampVerifyIfPresent,
			timestamp: at(-10 * time.Minute),
			wantErr:   signature.ErrTimestampOutsideWindow,
		},
		{
			name:      "verify-if-present rejects a malformed timestamp",
			mode:      signature.TimestampVerifyIfPresent,
			timestamp: "not-a-number",
			wantErr:   signature.ErrInvalidTimestamp,
		},

		// TimestampIgnore skips the check entirely, so even values that every
		// other mode rejects pass through.
		{name: "ignore accepts a missing timestamp", mode: signature.TimestampIgnore, timestamp: ""},
		{name: "ignore accepts a stale timestamp", mode: signature.TimestampIgnore, timestamp: at(-10 * time.Minute)},
		{name: "ignore accepts a malformed timestamp", mode: signature.TimestampIgnore, timestamp: "not-a-number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := signature.FreshnessPolicy{Mode: tt.mode}.Check(tt.timestamp, now)
			if tt.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFreshnessPolicy_Check_HonoursCustomWindow(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0)
	policy := signature.FreshnessPolicy{
		Mode:      signature.TimestampRequire,
		MaxAge:    10 * time.Second,
		ClockSkew: 2 * time.Second,
	}

	fresh := strconv.FormatInt(now.Add(-5*time.Second).Unix(), 10)
	require.NoError(t, policy.Check(fresh, now))

	// Older than the custom MaxAge but well inside the 5-minute SDK default,
	// so this only fails if Check uses the configured window.
	stale := strconv.FormatInt(now.Add(-30*time.Second).Unix(), 10)
	require.ErrorIs(t, policy.Check(stale, now), signature.ErrTimestampOutsideWindow)

	// Same for the skew: inside the 1-minute default, outside the configured 2s.
	future := strconv.FormatInt(now.Add(30*time.Second).Unix(), 10)
	require.ErrorIs(t, policy.Check(future, now), signature.ErrTimestampOutsideWindow)
}
