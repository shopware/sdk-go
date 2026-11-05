package resolver

import (
	"time"

	"github.com/shopware/sdk-go/signature"
)

// TimestampMode selects how strictly a signed request's timestamp is checked.
// Alias of signature.TimestampMode so every path shares one set of modes.
type TimestampMode = signature.TimestampMode

// Timestamp modes, re-exported from the signature package for convenience.
const (
	// TimestampUnset leaves the mode to the policy's documented default.
	TimestampUnset = signature.TimestampUnset

	TimestampRequire = signature.TimestampRequire

	TimestampVerifyIfPresent = signature.TimestampVerifyIfPresent

	TimestampIgnore = signature.TimestampIgnore
)

// QueryParamTimestamp is the query parameter Shopware signs the request time into.
const QueryParamTimestamp = "timestamp"

// QueryPolicy governs the non-signature checks for signed query authentication:
// request freshness and shop state. The zero value is the strictest policy, so
// QueryPolicy{} is always safe.
type QueryPolicy struct {
	// Default: TimestampRequire.
	Timestamp TimestampMode

	// When <= 0, signature.ReplayWindowSeconds (5 minutes) is used.
	MaxAge time.Duration

	// How far a timestamp may be dated into the future.
	// When <= 0, signature.ClockSkewSeconds (1 minute) is used.
	ClockSkew time.Duration

	// Only enable for routes that must work while a shop is deactivated;
	// anything representing an authenticated shop session must leave it false.
	AllowInactiveShop bool
}

// DefaultQueryPolicy returns the policy applied when none is configured.
func DefaultQueryPolicy() QueryPolicy {
	return QueryPolicy{
		Timestamp:         TimestampRequire,
		MaxAge:            signature.ReplayWindowSeconds * time.Second,
		ClockSkew:         signature.ClockSkewSeconds * time.Second,
		AllowInactiveShop: false,
	}
}

// withDefaults resolves an unset mode and fills unset durations.
func (p QueryPolicy) withDefaults() QueryPolicy {
	if p.Timestamp == TimestampUnset {
		p.Timestamp = TimestampRequire
	}

	freshness := p.freshness().WithDefaults()
	p.MaxAge = freshness.MaxAge
	p.ClockSkew = freshness.ClockSkew

	return p
}

// freshness projects the policy onto the shared freshness policy.
func (p QueryPolicy) freshness() signature.FreshnessPolicy {
	return signature.FreshnessPolicy{
		Mode:      p.Timestamp,
		MaxAge:    p.MaxAge,
		ClockSkew: p.ClockSkew,
	}
}

// PostPolicy governs the freshness check for signed JSON POST authentication.
// Its default is TimestampVerifyIfPresent because Shopware stamps a timestamp
// only into webhook payloads; requiring one would reject payment and gateway calls.
type PostPolicy struct {
	// TimestampUnset (the zero value) resolves to TimestampVerifyIfPresent;
	// TimestampRequire is honoured when set explicitly.
	Timestamp TimestampMode

	// When <= 0, signature.ReplayWindowSeconds (5 minutes) is used.
	MaxAge time.Duration

	// When <= 0, signature.ClockSkewSeconds (1 minute) is used.
	ClockSkew time.Duration
}

// DefaultPostPolicy returns the policy applied when none is configured.
func DefaultPostPolicy() PostPolicy {
	return PostPolicy{
		Timestamp: TimestampVerifyIfPresent,
		MaxAge:    signature.ReplayWindowSeconds * time.Second,
		ClockSkew: signature.ClockSkewSeconds * time.Second,
	}
}

// withDefaults resolves an unset mode and fills unset durations.
func (p PostPolicy) withDefaults() PostPolicy {
	if p.Timestamp == TimestampUnset {
		p.Timestamp = TimestampVerifyIfPresent
	}

	freshness := p.freshness().WithDefaults()
	p.MaxAge = freshness.MaxAge
	p.ClockSkew = freshness.ClockSkew

	return p
}

// freshness projects the policy onto the shared freshness policy.
func (p PostPolicy) freshness() signature.FreshnessPolicy {
	return signature.FreshnessPolicy{
		Mode:      p.Timestamp,
		MaxAge:    p.MaxAge,
		ClockSkew: p.ClockSkew,
	}
}

// LifecyclePolicy governs the freshness check for lifecycle webhook
// authentication. Its default is TimestampRequire, like webhook.Policy, because
// Shopware registers lifecycle routes as ordinary webhooks and stamps every
// delivery attempt.
//
// It covers freshness only. The event-to-route binding is not configurable, and
// lifecycle routes always serve inactive shops, so there is no
// AllowInactiveShop either.
type LifecyclePolicy struct {
	// TimestampUnset (the zero value) resolves to TimestampRequire.
	Timestamp TimestampMode

	// When <= 0, signature.ReplayWindowSeconds (5 minutes) is used.
	MaxAge time.Duration

	// When <= 0, signature.ClockSkewSeconds (1 minute) is used.
	ClockSkew time.Duration
}

// DefaultLifecyclePolicy returns the policy applied when none is configured.
func DefaultLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		Timestamp: TimestampRequire,
		MaxAge:    signature.ReplayWindowSeconds * time.Second,
		ClockSkew: signature.ClockSkewSeconds * time.Second,
	}
}

// withDefaults resolves an unset mode and fills unset durations.
func (p LifecyclePolicy) withDefaults() LifecyclePolicy {
	if p.Timestamp == TimestampUnset {
		p.Timestamp = TimestampRequire
	}

	freshness := p.freshness().WithDefaults()
	p.MaxAge = freshness.MaxAge
	p.ClockSkew = freshness.ClockSkew

	return p
}

// freshness projects the policy onto the shared freshness policy.
func (p LifecyclePolicy) freshness() signature.FreshnessPolicy {
	return signature.FreshnessPolicy{
		Mode:      p.Timestamp,
		MaxAge:    p.MaxAge,
		ClockSkew: p.ClockSkew,
	}
}
