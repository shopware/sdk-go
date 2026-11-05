package webhook_test

import (
	"testing"

	"github.com/shopware/sdk-go/webhook"
)

func TestVerifierInterface(_ *testing.T) {
	var _ webhook.Verifier = (*webhook.Middleware)(nil)
}
