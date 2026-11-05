package adminapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopware/sdk-go/clients/adminapi/tokenstorage"
)

// staticTransport answers every request with a fixed body, so benchmarks
// measure the client and not the network.
type staticTransport struct{}

func (staticTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"total":0,"data":[]}`)),
	}, nil
}

func BenchmarkRequestWithCachedToken(b *testing.B) {
	c, err := NewClient(Config{
		BaseURL:     "https://shop.example.com",
		Credentials: integration,
		HTTPClient:  &http.Client{Transport: staticTransport{}},
	})
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	if err = c.SetToken(ctx, tokenstorage.Token{AccessToken: "a", Expiry: time.Now().Add(time.Hour)}); err != nil {
		b.Fatal(err)
	}
	body := map[string]any{"limit": 1}

	b.ReportAllocs()
	for b.Loop() {
		if _, err = c.Post(ctx, "/search/product", body); err != nil {
			b.Fatal(err)
		}
	}
}
