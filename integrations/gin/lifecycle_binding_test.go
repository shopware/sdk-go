package gin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	ginintegration "github.com/shopware/sdk-go/integrations/gin"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signedLifecycleRequest builds a signed lifecycle POST for the given event and
// path. Pass a zero timestamp to omit the field.
func signedLifecycleRequest(
	t *testing.T,
	shop *store.Shop,
	event webhook.LifecycleEvent,
	path string,
	timestamp int64,
) *http.Request {
	t.Helper()

	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"url":    shop.ShopURL,
			"shopId": shop.ShopID,
		},
		"data": map[string]interface{}{
			"event":   string(event),
			"payload": []map[string]interface{}{},
		},
	}
	if timestamp != 0 {
		payload["timestamp"] = timestamp
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signature.ShopSignatureHeader, signature.ComputeSignature(body, shop.ShopSecret))

	return req
}

// A signed app.activated body sent to /app/delete must not delete the shop.
func TestLifecycleRoute_CrossRouteReplayRejected(t *testing.T) {
	t.Parallel()

	router := gin.New()
	testApp := createTestApp(t)
	require.NoError(t, ginintegration.Configure(router, ginintegration.Config{App: testApp}))

	shopID := "cross-route-replay-shop"
	shop := registerAndConfirmShop(t, testApp, router, shopID, "https://cross-route-replay.local")

	req := signedLifecycleRequest(t, shop, webhook.EventAppActivated, "/app/delete", time.Now().Unix())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorType(t, w.Body.Bytes(), sdkerrors.TypeEventMismatch)

	survivor, err := testApp.Store().GetShopByID(context.Background(), shopID)
	require.NoError(t, err, "shop must not be deleted by a mismatched event")
	assert.Equal(t, shopID, survivor.ShopID)
}

// Every route rejects every event that is not its own.
func TestLifecycleRoutes_RejectForeignEvents(t *testing.T) {
	t.Parallel()

	routes := map[string]webhook.LifecycleEvent{
		"/app/activate":   webhook.EventAppActivated,
		"/app/deactivate": webhook.EventAppDeactivated,
		"/app/delete":     webhook.EventAppDeleted,
	}

	for path, own := range routes {
		for _, foreign := range []webhook.LifecycleEvent{
			webhook.EventAppActivated,
			webhook.EventAppDeactivated,
			webhook.EventAppDeleted,
			"order.placed",
		} {
			if foreign == own {
				continue
			}

			t.Run(path+"_"+string(foreign), func(t *testing.T) {
				t.Parallel()

				router := gin.New()
				testApp := createTestApp(t)
				require.NoError(t, ginintegration.Configure(router, ginintegration.Config{App: testApp}))

				shopID := "foreign-event-shop"
				shop := registerAndConfirmShop(t, testApp, router, shopID, "https://foreign-event.local")

				req := signedLifecycleRequest(t, shop, foreign, path, time.Now().Unix())
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusForbidden, w.Code)
				assertErrorType(t, w.Body.Bytes(), sdkerrors.TypeEventMismatch)
			})
		}
	}
}

// Shopware stamps every delivery attempt, so a body without one is rejected.
func TestLifecycleRoute_MissingTimestampRejected(t *testing.T) {
	t.Parallel()

	router := gin.New()
	testApp := createTestApp(t)
	require.NoError(t, ginintegration.Configure(router, ginintegration.Config{App: testApp}))

	shop := registerAndConfirmShop(t, testApp, router, "no-timestamp-shop", "https://no-timestamp.local")

	req := signedLifecycleRequest(t, shop, webhook.EventAppActivated, "/app/activate", 0)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorType(t, w.Body.Bytes(), sdkerrors.TypeTimestampExpired)
}

func TestLifecycleRoute_StaleTimestampRejected(t *testing.T) {
	t.Parallel()

	router := gin.New()
	testApp := createTestApp(t)
	require.NoError(t, ginintegration.Configure(router, ginintegration.Config{App: testApp}))

	shopID := "stale-timestamp-shop"
	shop := registerAndConfirmShop(t, testApp, router, shopID, "https://stale-timestamp.local")

	stale := time.Now().Add(-1 * time.Hour).Unix()
	req := signedLifecycleRequest(t, shop, webhook.EventAppDeleted, "/app/delete", stale)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorType(t, w.Body.Bytes(), sdkerrors.TypeTimestampExpired)

	_, err := testApp.Store().GetShopByID(context.Background(), shopID)
	require.NoError(t, err, "shop must survive a stale delete")
}
