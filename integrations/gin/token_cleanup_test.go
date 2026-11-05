package gin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	adminstorage "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
	ginintegration "github.com/shopware/app-sdk-go/integrations/gin"
	"github.com/shopware/app-sdk-go/pkg/app"
	"github.com/shopware/app-sdk-go/pkg/signature"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingStorage records which keys are deleted.
type recordingStorage struct {
	*adminstorage.InMemory

	mu      sync.Mutex
	deleted []string
}

func (s *recordingStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	s.deleted = append(s.deleted, key)
	s.mu.Unlock()
	return s.InMemory.Delete(ctx, key)
}

func TestDeleteEndpoint_ClearsShopToken(t *testing.T) {
	t.Parallel()

	storage := &recordingStorage{InMemory: adminstorage.NewInMemory()}
	testApp, err := app.New(app.Config{
		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.example.com/app/register/confirm",
		ShopStore:       store.NewInMemoryStore(),
		TokenStorage:    storage,
	})
	require.NoError(t, err)

	router := gin.New()
	require.NoError(t, ginintegration.Configure(router, ginintegration.Config{App: testApp}))
	shop := registerAndConfirmShop(t, testApp, router, "token-cleanup-shop", "https://token-cleanup.local")

	// Seed a token, as if the app had already called the shop's API.
	client, err := testApp.NewClientForShop(shop)
	require.NoError(t, err)
	require.NoError(t, client.SetToken(context.Background(),
		adminstorage.Token{AccessToken: "token", Expiry: time.Now().Add(time.Hour)}))

	body, err := json.Marshal(map[string]any{
		"source":    map[string]any{"url": shop.ShopURL, "shopId": shop.ShopID},
		"data":      map[string]any{"event": "app.deleted", "payload": map[string]any{"keepUserData": false}},
		"timestamp": time.Now().Unix(),
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signature.ShopSignatureHeader, signature.ComputeSignature(body, shop.ShopSecret))
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	storage.mu.Lock()
	defer storage.mu.Unlock()
	require.Len(t, storage.deleted, 1)
	assert.True(t, strings.HasPrefix(storage.deleted[0], shop.ShopID+":"), "the shop's token key is deleted")

	_, err = storage.Get(context.Background(), storage.deleted[0])
	assert.ErrorIs(t, err, adminstorage.ErrNotFound)
}
