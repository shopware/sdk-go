package signature_test

import (
	"testing"

	"github.com/shopware/app-sdk-go/pkg/signature"
)

func BenchmarkVerifyRegistration(b *testing.B) {
	b.ReportAllocs()

	verifier := signature.NewVerifier()
	rawQuery := "shop-id=test&shop-url=https://test.com&timestamp=123"
	secret := "test-secret"
	sig := computeTestHMAC(b, rawQuery, secret)

	b.ResetTimer()
	for range b.N {
		verifier.VerifyRegistration(sig, rawQuery, secret)
	}
}

func BenchmarkVerifyWebhook(b *testing.B) {
	b.ReportAllocs()

	verifier := signature.NewVerifier()
	body := []byte(`{"test":"data"}`)
	secret := "test-secret"
	sig := computeTestHMAC(b, string(body), secret)

	b.ResetTimer()
	for range b.N {
		verifier.VerifyWebhook(sig, body, secret)
	}
}

func BenchmarkGenerateShopSecret(b *testing.B) {
	b.ReportAllocs()

	verifier := signature.NewVerifier()

	b.ResetTimer()
	for range b.N {
		verifier.GenerateShopSecret()
	}
}

func BenchmarkComputeSignature(b *testing.B) {
	b.ReportAllocs()

	data := []byte("test data for benchmarking")
	secret := "benchmark-secret"

	b.ResetTimer()
	for range b.N {
		signature.ComputeSignature(data, secret)
	}
}

func BenchmarkComputeSignature_LargePayload(b *testing.B) {
	b.ReportAllocs()

	data := make([]byte, 10000)
	for i := range data {
		data[i] = byte(i % 256)
	}
	secret := "benchmark-secret"

	b.ResetTimer()
	for range b.N {
		signature.ComputeSignature(data, secret)
	}
}
