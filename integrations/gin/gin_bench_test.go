package gin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	ginintegration "github.com/shopware/sdk-go/integrations/gin"
	"github.com/shopware/sdk-go/integrations/gin/internal/testutil"
	"github.com/shopware/sdk-go/signature"
)

func BenchmarkRegistrationEndpoint(b *testing.B) {
	b.ReportAllocs()

	router := gin.New()
	testApp := createTestApp(b)

	err := ginintegration.Configure(router, ginintegration.Config{
		App: testApp,
	})
	if err != nil {
		b.Fatal(err)
	}

	timestamp := time.Now().Unix()
	rawQuery := fmt.Sprintf("shop-id=bench&shop-url=https://bench&timestamp=%d", timestamp)
	sig := signature.ComputeSignature([]byte(rawQuery), "test-secret-1234567890")

	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/app/register?"+rawQuery, nil)
		req.Header.Set(signature.AppSignatureHeader, sig)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkWebhookVerification(b *testing.B) {
	b.ReportAllocs()

	router := gin.New()
	testApp := createTestApp(b)

	shop := testutil.NewTestShop()
	if err := testApp.Store().UpdateShop(context.Background(), shop); err != nil {
		b.Fatal(err)
	}

	err := ginintegration.Configure(router, ginintegration.Config{
		App: testApp,
	})
	if err != nil {
		b.Fatal(err)
	}

	webhookEvent := map[string]interface{}{
		"source": map[string]interface{}{
			"url":    shop.ShopURL,
			"shopId": shop.ShopID,
		},
		"data": map[string]interface{}{
			"event":   "benchmark.event",
			"payload": []map[string]interface{}{},
		},
	}

	webhookBody, err := json.Marshal(webhookEvent)
	if err != nil {
		b.Fatal(err)
	}
	webhookSig := signature.ComputeSignature(webhookBody, shop.ShopSecret)

	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(webhookBody))
		req.Header.Set(signature.ShopSignatureHeader, webhookSig)
		router.ServeHTTP(w, req)
	}
}
