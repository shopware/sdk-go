package adminapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/shopware/app-sdk-go/clients/adminapi"
)

// newExampleShop starts a tiny fake shop so the examples run without a real
// Shopware instance.
func newExampleShop() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/oauth/token":
			var fields map[string]string
			_ = json.NewDecoder(r.Body).Decode(&fields)
			resp := map[string]any{"access_token": "token", "expires_in": 600}
			if fields["grant_type"] == "password" {
				resp["refresh_token"] = "refresh"
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/search/product":
			_, _ = io.WriteString(w, `{"total":2,"data":[{"id":"a","name":"Shirt"},{"id":"b","name":"Cap"}]}`)
		default:
			_, _ = io.WriteString(w, `{"version":"6.7.0.0"}`)
		}
	}))
}

func exampleLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

// Log in with the client ID and secret of an integration or app, and search
// for products.
func ExampleNewClient() {
	shop := newExampleShop()
	defer shop.Close()

	client, err := adminapi.NewClient(adminapi.Config{
		BaseURL: shop.URL, // for example "https://shop.example.com"
		Credentials: adminapi.IntegrationCredentials{
			ClientID:     "SWIA...",
			ClientSecret: "secret",
		},
	})
	if err != nil {
		exampleLogger().Error("create client", "error", err)
		return
	}

	resp, err := client.Post(context.Background(), "/search/product", map[string]any{"limit": 10})
	if err != nil {
		exampleLogger().Error("search products", "error", err)
		return
	}

	var result struct {
		Total int `json:"total"`
		Data  []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err = resp.JSON(&result); err != nil {
		exampleLogger().Error("decode products", "error", err)
		return
	}
	fmt.Println(result.Total, result.Data[0].Name)
	// Output: 2 Shirt
}

// Log in as an admin user. After the first login the client uses the refresh
// token, so the password is not sent with every token refresh.
func ExampleNewClient_password() {
	shop := newExampleShop()
	defer shop.Close()

	client, err := adminapi.NewClient(adminapi.Config{
		BaseURL:     shop.URL,
		Credentials: adminapi.PasswordCredentials{Username: "admin", Password: "shopware"},
	})
	if err != nil {
		exampleLogger().Error("create client", "error", err)
		return
	}

	resp, err := client.Get(context.Background(), "/_info/version")
	if err != nil {
		exampleLogger().Error("get version", "error", err)
		return
	}
	fmt.Println(string(resp.Body))
	// Output: {"version":"6.7.0.0"}
}

// Build paths with Path when a segment comes from user input.
func ExamplePath() {
	fmt.Println(adminapi.Path("product", "0190e3a1"))
	fmt.Println(adminapi.Path("_action", "extension", "install", "plugin", "../../evil"))
	// Output:
	// /product/0190e3a1
	// /_action/extension/install/plugin/..%2F..%2Fevil
}

// Check the status of a failed request.
func ExampleIsNotFound() {
	err := error(&adminapi.APIError{StatusCode: http.StatusNotFound})
	fmt.Println(adminapi.IsNotFound(err))
	// Output: true
}
