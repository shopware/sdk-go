package signature

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

var (
	// ErrMissingTimestamp is returned when a request carries no timestamp to check.
	ErrMissingTimestamp = errors.New("missing timestamp")

	// ErrInvalidTimestamp is returned when a timestamp is present but not a Unix epoch integer.
	ErrInvalidTimestamp = errors.New("invalid timestamp format")

	// ErrTimestampOutsideWindow is returned when a timestamp is too old or too far in the future.
	ErrTimestampOutsideWindow = errors.New("timestamp outside replay window")
)

// CheckFreshness verifies that timestampStr (a Unix epoch in seconds) falls
// within [now-maxAge, now+skew].
//
// A timestamp that is only covered by an HMAC is integrity-protected but never
// expires on its own: replaying the same bytes reproduces the same valid
// signature forever. Comparing it against the current time is what turns it
// into a freshness guarantee, so callers must run this check on every
// authenticated request that carries a timestamp.
//
// Only call this after the signature itself has been verified — an unverified
// timestamp is attacker-controlled data.
//
// maxAge and skew fall back to DefaultReplayWindow and DefaultClockSkew when
// they are <= 0. Errors wrap ErrMissingTimestamp, ErrInvalidTimestamp or
// ErrTimestampOutsideWindow so callers can map them onto their own error types.
func CheckFreshness(timestampStr string, maxAge, skew time.Duration, now time.Time) error {
	if timestampStr == "" {
		return ErrMissingTimestamp
	}

	if maxAge <= 0 {
		maxAge = DefaultReplayWindow
	}
	if skew <= 0 {
		skew = DefaultClockSkew
	}

	seconds, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidTimestamp, timestampStr)
	}

	age := now.Sub(time.Unix(seconds, 0))
	if age > maxAge {
		return fmt.Errorf("%w: %s old, limit is %s", ErrTimestampOutsideWindow, age.Round(time.Second), maxAge)
	}
	if -age > skew {
		return fmt.Errorf("%w: %s in the future, limit is %s", ErrTimestampOutsideWindow, (-age).Round(time.Second), skew)
	}

	return nil
}
