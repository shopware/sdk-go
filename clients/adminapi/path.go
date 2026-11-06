package adminapi

import (
	"net/url"
	"strings"
)

// Path joins segments into an API path and escapes each segment, so values
// such as entity IDs or names from user input cannot change the path:
//
//	adminapi.Path("product", id) // "/product/<escaped id>"
//
// The segments "." and ".." are sent as "%2E" and "%2E%2E": url.PathEscape
// leaves them as they are, and servers and proxies resolve literal dot
// segments ("/product/.." would become "/").
//
// Always build paths with Path when a segment comes from outside your code.
func Path(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = escapeSegment(s)
	}
	return "/" + strings.Join(escaped, "/")
}

func escapeSegment(s string) string {
	if s == "." || s == ".." {
		return strings.ReplaceAll(s, ".", "%2E")
	}
	return url.PathEscape(s)
}
