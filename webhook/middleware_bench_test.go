package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
)

func BenchmarkVerifyWebhook(b *testing.B) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	shop := testutil.NewTestShop()
	if err := shopStore.UpdateShop(context.Background(), shop); err != nil {
		b.Fatal(err)
	}

	event := testutil.NewTestWebhookEvent(
		testutil.WithEventName("order.placed"),
		testutil.WithEntityData("order", "insert", "order-123"),
	)
	body, err := json.Marshal(event)
	if err != nil {
		b.Fatal(err)
	}
	sig := signature.ComputeSignature(body, shop.ShopSecret)

	b.ResetTimer()
	for i := range b.N {
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
		req.Header.Set(signature.ShopSignatureHeader, sig)

		middleware.VerifyWebhook(context.Background(), req, "trace-"+strconv.Itoa(i))
	}
}
