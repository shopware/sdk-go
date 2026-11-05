package webhook

import (
	"time"

	"github.com/shopware/sdk-go/signature"
)

// Policy governs the freshness check applied to webhook authentication.
type Policy struct {
	// Freshness governs how the webhook body's `timestamp` is validated.
	// The zero value requires a timestamp within 5 minutes, tolerating 1 minute
	// of future clock skew.
	Freshness signature.FreshnessPolicy
}

// DefaultPolicy returns the policy applied when none is configured: a timestamp
// is required, within signature.ReplayWindowSeconds, tolerating
// signature.ClockSkewSeconds of future skew.
func DefaultPolicy() Policy {
	return Policy{
		Freshness: signature.FreshnessPolicy{
			Mode:      signature.TimestampRequire,
			MaxAge:    signature.ReplayWindowSeconds * time.Second,
			ClockSkew: signature.ClockSkewSeconds * time.Second,
		},
	}
}

// withDefaults resolves an unset mode to TimestampRequire and fills unset
// durations with the SDK defaults.
func (p Policy) withDefaults() Policy {
	if p.Freshness.Mode == signature.TimestampUnset {
		p.Freshness.Mode = signature.TimestampRequire
	}
	p.Freshness = p.Freshness.WithDefaults()

	return p
}
