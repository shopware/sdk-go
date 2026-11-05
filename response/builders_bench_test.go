package response_test

import (
	"testing"

	"github.com/shopware/sdk-go/response"
)

func BenchmarkNotificationSuccess(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		_ = response.NotificationSuccess("Operation successful")
	}
}

func BenchmarkPaymentPaid(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		_ = response.PaymentPaid()
	}
}

func BenchmarkOpenNewTabResponse(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		_ = response.OpenNewTabResponse("https://example.com")
	}
}
