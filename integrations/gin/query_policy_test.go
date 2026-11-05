package gin_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	ginintegration "github.com/shopware/app-sdk-go/integrations/gin"
	"github.com/shopware/app-sdk-go/integrations/gin/internal/testutil"
	"github.com/shopware/app-sdk-go/pkg/app"
	sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"
	"github.com/shopware/app-sdk-go/pkg/resolver"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPolicyRouter wires a single GET route behind the given middleware and
// returns the router plus the shop stored for it.
func newPolicyRouter(t *testing.T, verifier gin.HandlerFunc, sdk *app.App, opts ...testutil.ShopOption) (*gin.Engine, *store.Shop) {
	t.Helper()

	shop := testutil.NewTestShop(opts...)
	require.NoError(t, sdk.Store().UpdateShop(context.Background(), shop))

	router := gin.New()
	router.GET("/custom", verifier, func(c *gin.Context) {
		resolved, exists := ginintegration.GetShop(c)
		require.True(t, exists)
		c.JSON(http.StatusOK, gin.H{"shop_id": resolved.ShopID})
	})

	return router, shop
}

func doSignedGet(router *gin.Engine, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/custom?"+query, nil))
	return w
}

func assertErrorType(t *testing.T, body []byte, wantType string) {
	t.Helper()

	var resp map[string]any
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, wantType, resp["type"])
}

// TestGetRequestVerifier_RejectsInactiveShop covers the second half of
// SEC-REPLAY-QUERY-L79: a signed URL used to keep authenticating a shop that
// had been deactivated.
func TestGetRequestVerifier_RejectsInactiveShop(t *testing.T) {
	t.Parallel()

	sdk := createTestApp(t)
	router, shop := newPolicyRouter(t,
		ginintegration.GetRequestVerifier(sdk, slog.New(slog.DiscardHandler)),
		sdk, testutil.WithShopActive(false))

	w := doSignedGet(router, testutil.SignedQuery("shop-id="+shop.ShopID, shop.ShopSecret))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorType(t, w.Body.Bytes(), sdkerrors.TypeShopNotActive)
}

func TestGetRequestVerifierWithPolicy_OverridesAppPolicy(t *testing.T) {
	t.Parallel()

	sdk := createTestApp(t)
	shop := testutil.NewTestShop()
	require.NoError(t, sdk.Store().UpdateShop(context.Background(), shop))

	log := slog.New(slog.DiscardHandler)
	router := gin.New()
	router.GET("/strict", ginintegration.GetRequestVerifier(sdk, log), okHandler)
	router.GET("/legacy", ginintegration.GetRequestVerifierWithPolicy(sdk, log, resolver.QueryPolicy{
		Timestamp: resolver.TimestampIgnore,
	}), okHandler)

	// A signed query with a long-expired timestamp: rejected on the default
	// route, accepted on the route that explicitly opted out.
	query := testutil.SignedQueryAt("shop-id="+shop.ShopID, shop.ShopSecret, time.Unix(1, 0))

	strict := httptest.NewRecorder()
	router.ServeHTTP(strict, httptest.NewRequest(http.MethodGet, "/strict?"+query, nil))
	assert.Equal(t, http.StatusUnauthorized, strict.Code)
	assertErrorType(t, strict.Body.Bytes(), sdkerrors.TypeTimestampExpired)

	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/legacy?"+query, nil))
	assert.Equal(t, http.StatusOK, legacy.Code)
}

func TestConfigure_PassesQueryPolicyToApp(t *testing.T) {
	t.Parallel()

	router := gin.New()
	require.NoError(t, ginintegration.Configure(router, ginintegration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret-1234567890",
		ShopStore: store.NewInMemoryStore(),
		QueryPolicy: resolver.QueryPolicy{
			Timestamp: resolver.TimestampVerifyIfPresent,
			MaxAge:    42 * time.Second,
		},
		InflightAllowanceSeconds: 90,
	}))
}

// TestGetRequestVerifier_ParityWithResolver pins the Gin middleware to the core
// resolver. The middleware used to duplicate the verification logic and drifted
// from it: it lost the previous-secret grace window and never checked freshness
// or shop state. Any future divergence fails here.
func TestGetRequestVerifier_ParityWithResolver(t *testing.T) {
	t.Parallel()

	rotatedAt := time.Now().Add(-30 * time.Second)

	tests := []struct {
		name       string
		shopOpts   []testutil.ShopOption
		buildQuery func(shop *store.Shop) string
		wantStatus int
	}{
		{
			name:       "fresh signed query",
			buildQuery: func(s *store.Shop) string { return testutil.SignedQuery("shop-id="+s.ShopID, s.ShopSecret) },
			wantStatus: http.StatusOK,
		},
		{
			name: "previous secret within rotation grace window",
			shopOpts: []testutil.ShopOption{
				testutil.WithPreviousShopSecret("old-secret"),
				testutil.WithSecretsRotatedAt(&rotatedAt),
			},
			buildQuery: func(s *store.Shop) string { return testutil.SignedQuery("shop-id="+s.ShopID, "old-secret") },
			wantStatus: http.StatusOK,
		},
		{
			name: "stale timestamp",
			buildQuery: func(s *store.Shop) string {
				return testutil.SignedQueryAt("shop-id="+s.ShopID, s.ShopSecret, time.Unix(1, 0))
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "inactive shop",
			shopOpts:   []testutil.ShopOption{testutil.WithShopActive(false)},
			buildQuery: func(s *store.Shop) string { return testutil.SignedQuery("shop-id="+s.ShopID, s.ShopSecret) },
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sdk := createTestApp(t)
			shop := testutil.NewTestShop(tt.shopOpts...)
			require.NoError(t, sdk.Store().UpdateShop(context.Background(), shop))
			query := tt.buildQuery(shop)

			router := gin.New()
			router.GET("/custom", ginintegration.GetRequestVerifier(sdk, slog.New(slog.DiscardHandler)), okHandler)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/custom?"+query, nil))

			// Same request straight through the core resolver.
			_, coreErr := sdk.Resolver().ResolveFromQuery(
				context.Background(),
				httptest.NewRequest(http.MethodGet, "http://example.com/custom?"+query, nil),
			)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantStatus == http.StatusOK {
				require.NoError(t, coreErr, "core resolver must accept what the Gin middleware accepts")
				return
			}

			require.Error(t, coreErr, "core resolver must reject what the Gin middleware rejects")
			var sdkErr *sdkerrors.Error
			require.ErrorAs(t, coreErr, &sdkErr)
			assert.Equal(t, tt.wantStatus, sdkErr.Code, "both paths must map to the same HTTP status")
		})
	}
}

func okHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
