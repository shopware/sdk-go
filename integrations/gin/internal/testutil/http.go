package testutil

import (
	"strconv"
	"time"

	"github.com/shopware/app-sdk-go/pkg/signature"
)

// SignedQueryAt builds the query string of a signed GET request: it appends a
// `timestamp` parameter set to ts and the resulting `shopware-shop-signature`,
// mirroring how Shopware signs module and action button URLs.
func SignedQueryAt(query, secret string, ts time.Time) string {
	stamped := query
	stamp := "timestamp=" + strconv.FormatInt(ts.Unix(), 10)
	if stamped == "" {
		stamped = stamp
	} else {
		stamped += "&" + stamp
	}

	return stamped + "&shopware-shop-signature=" + signature.ComputeSignature([]byte(stamped), secret)
}

// SignedQuery is SignedQueryAt with the current time, i.e. a request that
// passes the resolver's freshness check.
func SignedQuery(query, secret string) string {
	return SignedQueryAt(query, secret, time.Now())
}
