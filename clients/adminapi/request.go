package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
)

// maxDrainBytes is how much of a 401 response body is read before the retry,
// so the connection can be reused and the body can be reported.
const maxDrainBytes = 64 << 10

// RequestOption changes a single request.
type RequestOption func(*requestOptions)

type requestOptions struct {
	headers map[string]string
	query   url.Values
}

func resolveOptions(opts []RequestOption) requestOptions {
	o := requestOptions{headers: map[string]string{}, query: url.Values{}}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithHeader sets a request header. Header names are case-insensitive; when
// several options set the same header, the last one wins. It cannot replace
// the Authorization header, which the client always sets itself.
func WithHeader(key, value string) RequestOption {
	return func(o *requestOptions) { o.headers[http.CanonicalHeaderKey(key)] = value }
}

// WithQuery adds a query parameter to the request URL.
func WithQuery(key, value string) RequestOption {
	return func(o *requestOptions) { o.query.Add(key, value) }
}

// Get sends a GET request to /api + path.
func (c *Client) Get(ctx context.Context, path string, opts ...RequestOption) (*Response, error) {
	return c.Request(ctx, http.MethodGet, path, nil, opts...)
}

// Post sends a POST request with a JSON body to /api + path.
func (c *Client) Post(ctx context.Context, path string, body any, opts ...RequestOption) (*Response, error) {
	return c.Request(ctx, http.MethodPost, path, body, opts...)
}

// Put sends a PUT request with a JSON body to /api + path.
func (c *Client) Put(ctx context.Context, path string, body any, opts ...RequestOption) (*Response, error) {
	return c.Request(ctx, http.MethodPut, path, body, opts...)
}

// Patch sends a PATCH request with a JSON body to /api + path.
func (c *Client) Patch(ctx context.Context, path string, body any, opts ...RequestOption) (*Response, error) {
	return c.Request(ctx, http.MethodPatch, path, body, opts...)
}

// Delete sends a DELETE request to /api + path. Use Request for a DELETE with
// a body.
func (c *Client) Delete(ctx context.Context, path string, opts ...RequestOption) (*Response, error) {
	return c.Request(ctx, http.MethodDelete, path, nil, opts...)
}

// Request sends a request to /api + path. A non-nil body is encoded as JSON
// and sent with Content-Type application/json.
//
// path is used as given. Build it with Path when it contains values from
// outside your code.
func (c *Client) Request(ctx context.Context, method, path string, body any, opts ...RequestOption) (*Response, error) {
	var bodyFactory func() (io.Reader, error)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("adminapi: encode request body: %w", err)
		}
		bodyFactory = func() (io.Reader, error) { return bytes.NewReader(data), nil }
		// Put first, so a WithHeader option can still override it.
		opts = append([]RequestOption{WithHeader("Content-Type", "application/json")}, opts...)
	}
	return c.RequestRaw(ctx, method, path, bodyFactory, opts...)
}

// RequestRaw sends a request to /api + path with the body produced by
// bodyFactory, as is. Set Content-Type with WithHeader. Use it for file
// uploads and other non-JSON bodies.
//
// bodyFactory is called again when the request is retried after a 401, so it
// must return a fresh reader every time. A nil bodyFactory sends no body.
func (c *Client) RequestRaw(
	ctx context.Context, method, path string, bodyFactory func() (io.Reader, error), opts ...RequestOption,
) (*Response, error) {
	return c.do(ctx, method, path, bodyFactory, resolveOptions(opts), true)
}

func (c *Client) do(
	ctx context.Context, method, path string, bodyFactory func() (io.Reader, error), o requestOptions, retry bool,
) (*Response, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := c.newRequest(ctx, method, path, bodyFactory, o)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adminapi: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized && retry {
		// Reading the body lets the connection be reused, and keeps it for
		// the error below. Close it now, not in the deferred close: that runs
		// only after the retry returns, and a body larger than maxDrainBytes
		// would hold its connection during the retry. With a transport that
		// allows one connection per host, the retry's token request would
		// wait for it until the context ends.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxDrainBytes))
		_ = resp.Body.Close()
		if err = c.invalidateUsedToken(ctx, token); err != nil {
			// Without invalidation the retry would send the same rejected
			// token again and hide the storage problem behind a second 401.
			return nil, fmt.Errorf("%w (the token could not be invalidated: %w)", newAPIError(resp.StatusCode, body), err)
		}
		return c.do(ctx, method, path, bodyFactory, o, false)
	}

	body, err := c.readBody(resp)
	if err != nil {
		return nil, err
	}
	if err = checkStatus(resp, body); err != nil {
		return nil, err
	}
	return &Response{StatusCode: resp.StatusCode, Headers: resp.Header, Body: body}, nil
}

func (c *Client) newRequest(
	ctx context.Context, method, path string, bodyFactory func() (io.Reader, error), o requestOptions,
) (*http.Request, error) {
	var body io.Reader
	if bodyFactory != nil {
		var err error
		if body, err = bodyFactory(); err != nil {
			return nil, fmt.Errorf("adminapi: build request body: %w", err)
		}
	}

	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := c.baseURL + "/api" + path
	if len(o.query) > 0 {
		u, err := url.Parse(target)
		if err != nil {
			return nil, fmt.Errorf("adminapi: invalid path %q: %w", path, err)
		}
		q := u.Query()
		for key, values := range o.query {
			for _, v := range values {
				q.Add(key, v)
			}
		}
		u.RawQuery = q.Encode()
		target = u.String()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("adminapi: build request: %w", err)
	}
	c.setCommonHeaders(req)
	for key, value := range o.headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

// setCommonHeaders sets the headers of every request, token requests
// included. Later headers win: User-Agent, then Config.Headers, then Accept.
func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.userAgent)
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
}

func (c *Client) readBody(resp *http.Response) ([]byte, error) {
	// Read one byte more than allowed to tell "exactly the limit" from "too
	// large". The +1 saturates: math.MaxInt64 + 1 would wrap to a negative
	// limit, and io.LimitReader would then return no body at all.
	limit := c.maxBody
	if limit < math.MaxInt64 {
		limit++
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("adminapi: read response: %w", err)
	}
	if int64(len(body)) > c.maxBody {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrResponseTooLarge, c.maxBody)
	}
	return body, nil
}

func checkStatus(resp *http.Response, body []byte) error {
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return fmt.Errorf("%w: status %d to %q", ErrUnexpectedRedirect, resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return newAPIError(resp.StatusCode, body)
	}
	return nil
}
