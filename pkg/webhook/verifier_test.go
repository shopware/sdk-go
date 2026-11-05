package webhook_test

import (
	"testing"

	"github.com/shopware/app-sdk-go/pkg/webhook"
)

func TestVerifierInterface(_ *testing.T) {
	var _ webhook.Verifier = (*webhook.Middleware)(nil)
}
