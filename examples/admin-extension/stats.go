package main

import (
	"context"
	"fmt"
	"time"

	"github.com/shopware/app-sdk-go/clients/adminapi"
)

// exactTotalCount asks the DAL for the exact number of matching rows.
const exactTotalCount = 1

// dashboardStats is the payload of GET /api/dashboard/stats. The field names
// match what the frontend expects (frontend/src/services/api.ts).
type dashboardStats struct {
	TotalOrders    int     `json:"totalOrders"`
	TotalRevenue   float64 `json:"totalRevenue"`
	ActiveProducts int     `json:"activeProducts"`
	LastSync       int64   `json:"lastSync"`
}

// loadDashboardStats reads the numbers of the dashboard from the shop's Admin
// API. Both requests use the same client, so they share one OAuth token.
//
// The criteria are plain JSON here; the typed DAL helpers of the SDK will
// replace them later.
func loadDashboardStats(ctx context.Context, client *adminapi.Client) (dashboardStats, error) {
	var orders struct {
		Total        int `json:"total"`
		Aggregations struct {
			Revenue struct {
				Sum float64 `json:"sum"`
			} `json:"revenue"`
		} `json:"aggregations"`
	}
	// The revenue is the plain sum of amountTotal over all orders, without
	// currency conversion; good enough for an example.
	err := search(ctx, client, "order", map[string]any{
		"limit":            1,
		"total-count-mode": exactTotalCount,
		"includes":         map[string]any{"order": []string{"id"}},
		"aggregations": []map[string]any{
			{"name": "revenue", "type": "sum", "field": "amountTotal"},
		},
	}, &orders)
	if err != nil {
		return dashboardStats{}, err
	}

	var products struct {
		Total int `json:"total"`
	}
	err = search(ctx, client, "product", map[string]any{
		"limit":            1,
		"total-count-mode": exactTotalCount,
		"includes":         map[string]any{"product": []string{"id"}},
		"filter": []map[string]any{
			{"type": "equals", "field": "active", "value": true},
		},
	}, &products)
	if err != nil {
		return dashboardStats{}, err
	}

	return dashboardStats{
		TotalOrders:    orders.Total,
		TotalRevenue:   orders.Aggregations.Revenue.Sum,
		ActiveProducts: products.Total,
		LastSync:       time.Now().Unix(),
	}, nil
}

// search runs a DAL search (POST /api/search/{entity}) and decodes the result.
func search(ctx context.Context, client *adminapi.Client, entity string, criteria map[string]any, result any) error {
	resp, err := client.Post(ctx, adminapi.Path("search", entity), criteria)
	if err != nil {
		return fmt.Errorf("search %s: %w", entity, err)
	}
	if err = resp.JSON(result); err != nil {
		return fmt.Errorf("decode %s search result: %w", entity, err)
	}
	return nil
}

// shopwareVersion returns the version of the shop, for example "6.7.0.0".
func shopwareVersion(ctx context.Context, client *adminapi.Client) (string, error) {
	resp, err := client.Get(ctx, "/_info/version")
	if err != nil {
		return "", err
	}
	var info struct {
		Version string `json:"version"`
	}
	if err = resp.JSON(&info); err != nil {
		return "", fmt.Errorf("decode version: %w", err)
	}
	return info.Version, nil
}
