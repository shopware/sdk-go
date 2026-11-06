package signature

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstantTimeCompare(t *testing.T) {
	t.Parallel()
	verifier := &HMACVerifier{}

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "equal strings",
			a:    "test-string",
			b:    "test-string",
			want: true,
		},
		{
			name: "different strings",
			a:    "test-string",
			b:    "different-string",
			want: false,
		},
		{
			name: "different length",
			a:    "short",
			b:    "longer-string",
			want: false,
		},
		{
			name: "empty strings",
			a:    "",
			b:    "",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := verifier.constantTimeCompare(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComputeSignature_ConsistentWithInternalCompute(t *testing.T) {
	data := []byte("test data")
	secret := "test-secret"

	publicSig := ComputeSignature(data, secret)

	verifier := &HMACVerifier{}
	internalSig := verifier.computeHMAC(data, secret)

	assert.Equal(t, internalSig, publicSig)
}
