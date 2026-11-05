package gin

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shopware/sdk-go/app"
	"github.com/shopware/sdk-go/integrations/gin/internal/testutil"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
)

//nolint:gochecknoinits // Required to set Gin to test mode before any tests run
func init() {
	gin.SetMode(gin.TestMode)
}

func TestBuildExemptPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		basePath  string
		reg       bool
		lifecycle bool
		want      []string
	}{
		{
			name:     "registration only",
			basePath: "/internal/api/lifecycle",
			reg:      true,
			want: []string{
				"/internal/api/lifecycle/register",
				"/internal/api/lifecycle/register/confirm",
			},
		},
		{
			name:      "lifecycle only",
			basePath:  "/api/lifecycle",
			lifecycle: true,
			want: []string{
				"/api/lifecycle/activate",
				"/api/lifecycle/deactivate",
				"/api/lifecycle/delete",
			},
		},
		{
			name:      "empty base path returns nil",
			basePath:  "",
			reg:       true,
			lifecycle: true,
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exempt := buildExemptPaths(tt.basePath, tt.reg, tt.lifecycle)
			if tt.want == nil {
				assert.Nil(t, exempt)
			} else {
				assert.ElementsMatch(t, tt.want, exempt)
			}
		})
	}
}

func TestSignatureVerificationMiddlewareWithExemptPaths_ExemptPath(t *testing.T) {
	t.Parallel()

	router := gin.New()

	sdk, err := app.New(app.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret-1234567890",
		ShopStore: store.NewInMemoryStore(),
	})
	require.NoError(t, err)

	router.Use(signatureVerificationMiddlewareWithExemptPaths(sdk, slog.New(slog.DiscardHandler), []string{"/exempt"}))
	router.GET("/exempt", func(c *gin.Context) {
		c.Header("X-OK", "1")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/exempt", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "1", w.Header().Get("X-Ok"))
}

func TestSignatureVerificationMiddlewareWithExemptPaths_GetRequest(t *testing.T) {
	t.Parallel()

	shopStore := store.NewInMemoryStore()
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	sdk, err := app.New(app.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret-1234567890",
		ShopStore: shopStore,
	})
	require.NoError(t, err)

	router := gin.New()
	router.Use(signatureVerificationMiddlewareWithExemptPaths(sdk, slog.New(slog.DiscardHandler), nil))
	router.GET("/custom", func(c *gin.Context) {
		shopFromCtx, exists := GetShop(c)
		if !exists || shopFromCtx.ShopID == "" {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
	})

	finalQuery := testutil.SignedQuery("shop-id="+shop.ShopID, shop.ShopSecret)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/custom?"+finalQuery, nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSignatureVerificationMiddlewareWithExemptPaths_PostInvalidSignature(t *testing.T) {
	t.Parallel()

	shopStore := store.NewInMemoryStore()
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	sdk, err := app.New(app.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret-1234567890",
		ShopStore: shopStore,
	})
	require.NoError(t, err)

	router := gin.New()
	router.Use(signatureVerificationMiddlewareWithExemptPaths(sdk, slog.New(slog.DiscardHandler), nil))
	router.POST("/custom", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	event := testutil.NewTestWebhookEvent(
		testutil.WithShopIDInEvent(shop.ShopID),
		testutil.WithShopURLInEvent(shop.ShopURL),
	)
	body, err := json.Marshal(event)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/custom", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signature.ShopSignatureHeader, "invalid-signature")
	router.ServeHTTP(w, req)

	assert.GreaterOrEqual(t, w.Code, http.StatusBadRequest)
}

func TestSignatureVerificationMiddlewareWithExemptPaths_PostValidSignature(t *testing.T) {
	t.Parallel()

	shopStore := store.NewInMemoryStore()
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	sdk, err := app.New(app.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret-1234567890",
		ShopStore: shopStore,
	})
	require.NoError(t, err)

	router := gin.New()
	router.Use(signatureVerificationMiddlewareWithExemptPaths(sdk, slog.New(slog.DiscardHandler), nil))
	router.POST("/custom", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	event := testutil.NewTestWebhookEvent(
		testutil.WithShopIDInEvent(shop.ShopID),
		testutil.WithShopURLInEvent(shop.ShopURL),
	)
	body, err := json.Marshal(event)
	require.NoError(t, err)
	sig := signature.ComputeSignature(body, shop.ShopSecret)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/custom", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signature.ShopSignatureHeader, sig)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())

	// Assert on the committed response (wire).
	res := w.Result()
	defer res.Body.Close()
	responseSig := res.Header.Get(signature.AppSignatureHeader)
	expectedSig := signature.ComputeSignature([]byte("ok"), shop.ShopSecret)
	assert.Equal(t, expectedSig, responseSig)
}

func TestResponseBodyWriter_WriteString(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	bodyWriter := &responseBodyWriter{
		ResponseWriter: ctx.Writer,
		body:           bytes.NewBuffer(nil),
	}

	// Buffers only; nothing on the wire before flush.
	_, err := bodyWriter.WriteString("hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", bodyWriter.body.String())
	assert.Empty(t, recorder.Body.String())

	bodyWriter.flush()
	assert.Equal(t, "hello", recorder.Body.String())
}

func TestResponseBodyWriter_Write(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	bodyWriter := &responseBodyWriter{
		ResponseWriter: ctx.Writer,
		body:           bytes.NewBuffer(nil),
	}

	// Header set after Write must still reach the wire.
	_, err := bodyWriter.Write([]byte("payload"))
	require.NoError(t, err)
	assert.Empty(t, recorder.Body.String())

	bodyWriter.Header().Set("X-Test-Header", "late")
	bodyWriter.flush()

	assert.Equal(t, "payload", recorder.Body.String())
	assert.Equal(t, "late", recorder.Result().Header.Get("X-Test-Header"))
}

// Flush() and WriteHeaderNow() are suppressed so a handler cannot commit the
// response before signing; only the internal flush() commits.
func TestResponseBodyWriter_CommitsSuppressedUntilFlush(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	bodyWriter := &responseBodyWriter{
		ResponseWriter: ctx.Writer,
		body:           bytes.NewBuffer(nil),
	}

	_, err := bodyWriter.WriteString("hello")
	require.NoError(t, err)

	// Explicit Flush / WriteHeaderNow must NOT commit anything to the wire.
	bodyWriter.Flush()
	bodyWriter.WriteHeaderNow()
	assert.False(t, recorder.Flushed)
	assert.Empty(t, recorder.Body.String())

	// Only the internal flush() commits the buffered body.
	bodyWriter.flush()
	assert.Equal(t, "hello", recorder.Body.String())
}

func TestBuildConfirmationURL_Configured(t *testing.T) {
	t.Parallel()

	configured := "https://example.com/custom/confirm"

	actual := buildConfirmationURL(&http.Request{}, configured, "/app")

	assert.Equal(t, configured, actual)
}

func TestCorsMiddleware_Options(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(corsMiddleware(nil))
	router.POST("/cors", func(c *gin.Context) {
		c.Header("X-Handler", "hit")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	req.Header.Set("Origin", "https://example.com")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Header().Get("X-Handler"))
}
