package registration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/shopware/sdk-go/registration"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
)

func BenchmarkHandleRegistration(b *testing.B) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "BenchApp",
		AppSecret: "bench-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	b.ResetTimer()
	for i := range b.N {
		shopID := "bench-shop-" + strconv.Itoa(i)
		ts := time.Now().Unix()
		rawQuery := fmt.Sprintf("shop-id=%s&shop-url=https://bench-shop.com&timestamp=%d", shopID, ts)
		req := &registration.Request{
			ShopID:       shopID,
			ShopURL:      "https://bench-shop.com",
			Timestamp:    ts,
			RawQuery:     rawQuery,
			AppSignature: signature.ComputeSignature([]byte(rawQuery), "bench-secret"),
		}
		handler.HandleRegistration(context.Background(), req)
	}
}

func BenchmarkHandleConfirmation(b *testing.B) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "BenchApp",
		AppSecret: "bench-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	shops := make([]string, b.N)
	secrets := make([]string, b.N)
	for i := range b.N {
		shopID := "bench-shop-" + strconv.Itoa(i)
		ts := time.Now().Unix()
		rawQuery := fmt.Sprintf("shop-id=%s&shop-url=https://bench-shop.com&timestamp=%d", shopID, ts)
		req := &registration.Request{
			ShopID:       shopID,
			ShopURL:      "https://bench-shop.com",
			Timestamp:    ts,
			RawQuery:     rawQuery,
			AppSignature: signature.ComputeSignature([]byte(rawQuery), "bench-secret"),
		}
		resp, err := handler.HandleRegistration(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		shops[i] = "bench-shop-" + strconv.Itoa(i)
		secrets[i] = resp.Secret
	}

	b.ResetTimer()
	for i := range b.N {
		confirmReq := registration.ConfirmationRequest{
			ShopID:    shops[i],
			ShopURL:   "https://bench-shop.com",
			APIKey:    "bench-api-key",
			SecretKey: "bench-secret-key",
			Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
		}

		body, err := json.Marshal(confirmReq)
		if err != nil {
			b.Fatal(err)
		}
		confirmReq.RawBody = body
		confirmReq.Signature = signature.ComputeSignature(body, secrets[i])

		handler.HandleConfirmation(context.Background(), &confirmReq)
	}
}
