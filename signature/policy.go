package signature

import "time"

// TimestampMode selects how strictly the timestamp of a signed request is checked.
type TimestampMode int

const (
	// TimestampUnset is the zero value: the mode was not chosen, so each policy
	// resolves it to that policy's documented default.
	TimestampUnset TimestampMode = iota

	// TimestampRequire rejects requests without a timestamp and requests whose
	// timestamp is outside the freshness window.
	TimestampRequire

	// TimestampVerifyIfPresent checks the timestamp when the request carries one
	// and accepts the request when it does not. Use it where Shopware provably
	// does not send a timestamp, so requiring one would break the flow.
	TimestampVerifyIfPresent

	// TimestampIgnore disables the freshness check entirely. A captured signed
	// request then authenticates forever, so pair it with a nonce, a short-lived
	// token exchange, or endpoint-specific authorization.
	TimestampIgnore
)

// FreshnessPolicy describes how a signed request's timestamp is validated.
//
// A timestamp covered by an HMAC is integrity-protected but does not expire on
// its own: replaying the same bytes reproduces the same valid signature forever.
// This policy is what turns it into a freshness guarantee.
//
// The zero value fails closed: an unset mode is checked as TimestampRequire and
// the SDK defaults for window and skew apply.
type FreshnessPolicy struct {
	Mode TimestampMode

	// MaxAge is how long a signed request stays valid.
	// When <= 0, DefaultReplayWindow is used.
	MaxAge time.Duration

	// ClockSkew is how far a timestamp may be dated into the future, covering
	// clock drift between shop and app server.
	// When <= 0, DefaultClockSkew is used.
	ClockSkew time.Duration
}

// WithDefaults fills unset durations with the SDK defaults.
func (p FreshnessPolicy) WithDefaults() FreshnessPolicy {
	if p.MaxAge <= 0 {
		p.MaxAge = DefaultReplayWindow
	}
	if p.ClockSkew <= 0 {
		p.ClockSkew = DefaultClockSkew
	}
	return p
}

// Check applies the policy to a timestamp value.
//
// Only call this after the signature has been verified — an unverified timestamp
// is attacker-controlled data.
//
// Errors wrap ErrMissingTimestamp, ErrInvalidTimestamp or
// ErrTimestampOutsideWindow so callers can map them onto their own error types.
func (p FreshnessPolicy) Check(timestampStr string, now time.Time) error {
	switch p.Mode {
	case TimestampIgnore:
		return nil
	case TimestampVerifyIfPresent:
		if timestampStr == "" {
			return nil
		}
	case TimestampUnset, TimestampRequire:
		if timestampStr == "" {
			return ErrMissingTimestamp
		}
	}

	effective := p.WithDefaults()

	return CheckFreshness(timestampStr, effective.MaxAge, effective.ClockSkew, now)
}
