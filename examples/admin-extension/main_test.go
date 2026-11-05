package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/shopware/app-sdk-go/examples/admin-extension/auth"
	"github.com/shopware/app-sdk-go/pkg/app"
	"github.com/shopware/app-sdk-go/pkg/store"
)

// fakeShopware answers the Admin API requests of the dashboard.
type fakeShopware struct {
	*httptest.Server

	tokenCalls   atomic.Int32
	failSearches atomic.Bool
	productQuery atomic.Value // last /search/product body
}

func newFakeShopware(t *testing.T) *fakeShopware {
	t.Helper()
	f := &fakeShopware{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/oauth/token":
			f.tokenCalls.Add(1)
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":600}`)
		case "/api/search/order":
			if f.failSearches.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, `{"total":3,"aggregations":{"revenue":{"sum":99.5}}}`)
		case "/api/search/product":
			body, _ := io.ReadAll(r.Body)
			f.productQuery.Store(string(body))
			_, _ = io.WriteString(w, `{"total":7}`)
		case "/api/_info/version":
			_, _ = io.WriteString(w, `{"version":"6.7.1.0"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// newTestRouter serves the protected API routes for one shop, with the
// frontend authentication replaced by a fixed shop context.
func newTestRouter(t *testing.T, shopURL string) *gin.Engine {
	t.Helper()
	shopStore := store.NewInMemoryStore()
	if err := shopStore.UpdateShop(context.Background(), &store.Shop{
		ShopID:       "shop-1",
		ShopURL:      shopURL,
		ShopSecret:   "shop-secret",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Active:       true,
	}); err != nil {
		t.Fatal(err)
	}
	sdk, err := app.New(app.Config{
		AppName:   "AdminExtensionApp",
		AppSecret: "test-secret-1234567890",
		ShopStore: shopStore,
	})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.DiscardHandler)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("shopContext", &auth.ShopContext{ShopID: "shop-1", ShopURL: shopURL})
		c.Set("shopID", "shop-1")
	})
	router.GET("/api/shop/info", getShopInfo(sdk, logger))
	router.GET("/api/dashboard/stats", getDashboardStats(sdk, logger))
	return router
}

func get(t *testing.T, router http.Handler, path string, target any) int {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %s: %v (body %q)", path, err, w.Body.String())
	}
	return w.Code
}

func TestDashboardStatsComeFromTheAdminAPI(t *testing.T) {
	shop := newFakeShopware(t)
	router := newTestRouter(t, shop.URL)

	var stats dashboardStats
	code := get(t, router, "/api/dashboard/stats", &stats)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if stats.TotalOrders != 3 || stats.TotalRevenue != 99.5 || stats.ActiveProducts != 7 || stats.LastSync == 0 {
		t.Errorf("stats = %+v", stats)
	}
	if got := shop.tokenCalls.Load(); got != 1 {
		t.Errorf("token requests = %d, want 1 (both searches share the token)", got)
	}

	var query struct {
		Filter []map[string]any `json:"filter"`
	}
	if err := json.Unmarshal([]byte(shop.productQuery.Load().(string)), &query); err != nil {
		t.Fatal(err)
	}
	if len(query.Filter) != 1 || query.Filter[0]["field"] != "active" || query.Filter[0]["value"] != true {
		t.Errorf("product filter = %v, want active = true", query.Filter)
	}
}

func TestDashboardStatsReportShopErrors(t *testing.T) {
	shop := newFakeShopware(t)
	shop.failSearches.Store(true)
	router := newTestRouter(t, shop.URL)

	var body map[string]string
	code := get(t, router, "/api/dashboard/stats", &body)

	if code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", code)
	}
	if body[keyError] == "" {
		t.Error("expected an error message")
	}
}

func TestShopInfoIncludesShopwareVersion(t *testing.T) {
	shop := newFakeShopware(t)
	router := newTestRouter(t, shop.URL)

	var info map[string]any
	code := get(t, router, "/api/shop/info", &info)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if info["shopId"] != "shop-1" || info["shopwareVersion"] != "6.7.1.0" {
		t.Errorf("info = %v", info)
	}
}

func TestShopInfoWithoutReachableShop(t *testing.T) {
	shop := newFakeShopware(t)
	router := newTestRouter(t, shop.URL)
	shop.Close()

	var info map[string]any
	code := get(t, router, "/api/shop/info", &info)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the version is optional", code)
	}
	if _, ok := info["shopwareVersion"]; ok {
		t.Errorf("unexpected version in %v", info)
	}
}
