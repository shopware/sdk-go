package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

const (
	// tokenExpiryMargin treats a token as expired this long before its real
	// expiry, so it does not run out in the middle of a request.
	tokenExpiryMargin = 30 * time.Second

	// defaultTokenLifetime is used when the shop sends no expires_in.
	defaultTokenLifetime = 10 * time.Minute

	// tokenFetchTimeout bounds a shared token fetch, including waiting for
	// the Locker. The fetch does not use the caller's cancellation, because
	// other callers may be waiting for it.
	tokenFetchTimeout = time.Minute

	grantTypeRefreshToken = "refresh_token"

	// errorInvalidGrant is the OAuth error code for a rejected grant, such as
	// an expired or revoked refresh token (RFC 6749, section 5.2).
	errorInvalidGrant = "invalid_grant"

	// Shopware reports OAuth errors in its own error format, with the numeric
	// code of league/oauth2-server instead of the OAuth error code:
	// 8 is invalidRefreshToken and 10 is invalidGrant, both invalid_grant.
	shopwareInvalidRefreshToken = "8"
	shopwareInvalidGrant        = "10"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Authenticate gets a token now instead of on the first request. Use it to
// check the credentials early.
func (c *Client) Authenticate(ctx context.Context) error {
	_, err := c.getToken(ctx)
	return err
}

// AccessToken returns a valid access token, fetching a new one when needed.
// Use it to hand the token to other tools; requests through the Client need
// no manual token handling.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	return c.getToken(ctx)
}

// SetToken stores a token obtained elsewhere, for example by a browser login.
// It waits for a token refresh in progress, so the refresh cannot overwrite
// the new token afterwards.
func (c *Client) SetToken(ctx context.Context, token tokenstorage.Token) error {
	unlock, err := c.lockToken(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err = c.setToken(ctx, token); err != nil {
		// Keep a pending token: it may hold the only valid refresh token.
		return err
	}
	c.pending = nil // the stored token replaces the token not stored yet
	return nil
}

// InvalidateToken drops the access token, so the next request gets a new one.
// A refresh token is kept and used for that.
func (c *Client) InvalidateToken(ctx context.Context) error {
	unlock, err := c.lockToken(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	stored, err := c.loadReconciled(ctx)
	if err != nil {
		return err
	}
	return c.dropAccessToken(ctx, stored)
}

// ClearToken removes the stored token completely, including the refresh
// token. The next request logs in again. It waits for a token refresh in
// progress, so the refresh cannot store a token again afterwards.
func (c *Client) ClearToken(ctx context.Context) error {
	unlock, err := c.lockToken(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err = c.clearToken(ctx); err != nil {
		// The logout failed; a pending token may hold the only valid
		// refresh token, so it is kept.
		return err
	}
	c.pending = nil // a logout must not be undone by a token not stored yet
	return nil
}

// setToken stores token. The caller holds the token lock.
func (c *Client) setToken(ctx context.Context, token tokenstorage.Token) error {
	if err := c.storage.Set(ctx, c.tokenKey, token); err != nil {
		return fmt.Errorf("adminapi: store token: %w", err)
	}
	return nil
}

// clearToken deletes the stored token. The caller holds the token lock.
func (c *Client) clearToken(ctx context.Context) error {
	if err := c.storage.Delete(ctx, c.tokenKey); err != nil {
		return fmt.Errorf("adminapi: delete token: %w", err)
	}
	return nil
}

func (c *Client) getToken(ctx context.Context) (string, error) {
	// Do not start a shared fetch for a caller that has already given up.
	if err := ctx.Err(); err != nil {
		return "", err
	}

	stored, err := c.loadToken(ctx)
	if err != nil {
		return "", err
	}
	if c.valid(stored) {
		return stored.AccessToken, nil
	}

	// Only one fetch per key runs at a time; concurrent callers share it.
	// Each caller still stops waiting when its own context is done.
	ch := c.tokenGroup.DoChan(c.tokenKey, func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenFetchTimeout)
		defer cancel()
		return c.refreshToken(fetchCtx)
	})

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return "", res.Err
		}
		token, ok := res.Val.(string)
		if !ok {
			return "", errors.New("adminapi: unexpected token fetch result")
		}
		return token, nil
	}
}

// refreshToken gets a new token and stores it. It runs once per key at a
// time within this process, and once across processes when a Locker is set.
func (c *Client) refreshToken(ctx context.Context) (string, error) {
	unlock, err := c.lockToken(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()

	// Another caller or process may have stored a fresh token meanwhile.
	stored, err := c.loadReconciled(ctx)
	if err != nil {
		return "", err
	}
	if c.valid(stored) {
		return stored.AccessToken, nil
	}

	fetched, err := c.fetchToken(ctx, stored)
	if err != nil {
		return "", err
	}
	if fetched.reused {
		return fetched.token.AccessToken, nil
	}

	// Access and refresh token are written together, before the token is
	// used. Shopware revokes the old refresh token on every refresh, so a
	// new refresh token that is not stored would be lost: keep it and store
	// it on the next refresh instead of exchanging the revoked one again.
	if err = c.setToken(ctx, fetched.token); err != nil {
		c.pending = &pendingToken{token: fetched.token, replaces: fetched.replaces}
		return "", err
	}
	return fetched.token.AccessToken, nil
}

// loadReconciled reads the stored token after storing a pending token, so
// code that changes the stored token works on the newest one. Without this,
// an invalidation would rewrite the old token, and storePending would then
// take that change for a newer token and drop the pending one, which may
// hold the only valid refresh token. The caller holds the token lock.
func (c *Client) loadReconciled(ctx context.Context) (tokenstorage.Token, error) {
	stored, err := c.loadToken(ctx)
	if err != nil {
		return tokenstorage.Token{}, err
	}
	return c.storePending(ctx, stored)
}

// storePending stores a token whose earlier write failed, and returns the
// token now in the storage. The pending token is dropped when the storage
// changed in the meantime (another process or SetToken stored a token), as
// that token is newer. The caller holds the token lock.
func (c *Client) storePending(ctx context.Context, stored tokenstorage.Token) (tokenstorage.Token, error) {
	if c.pending == nil {
		return stored, nil
	}
	if !sameToken(stored, c.pending.replaces) {
		c.pending = nil
		return stored, nil
	}
	if err := c.setToken(ctx, c.pending.token); err != nil {
		return tokenstorage.Token{}, err
	}
	stored = c.pending.token
	c.pending = nil
	return stored, nil
}

func sameToken(a, b tokenstorage.Token) bool {
	return a.AccessToken == b.AccessToken && a.RefreshToken == b.RefreshToken && a.Expiry.Equal(b.Expiry)
}

// fetchResult is the outcome of fetchToken.
type fetchResult struct {
	// token is the new token, or the stored token when reused is set.
	token tokenstorage.Token

	// reused reports that token is already stored.
	reused bool

	// replaces is the stored token the new token was issued against: the
	// snapshot passed to fetchToken, or the one it read again after the shop
	// rejected the refresh token. A pending token is compared with it.
	replaces tokenstorage.Token
}

// fetchToken requests a token with the refresh token of stored. If the shop
// rejects it, fetchToken checks whether another process stored a newer token,
// and otherwise asks the credentials once more without the rejected token.
func (c *Client) fetchToken(ctx context.Context, stored tokenstorage.Token) (fetchResult, error) {
	refreshToken := stored.RefreshToken
	token, err := c.requestToken(ctx, refreshToken)
	if err == nil || refreshToken == "" || !isRejectedGrant(err) {
		return fetchResult{token: token, replaces: stored}, err
	}

	current, err := c.loadToken(ctx)
	if err != nil {
		return fetchResult{}, err
	}
	if current.RefreshToken != refreshToken && c.valid(current) {
		return fetchResult{token: current, reused: true, replaces: current}, nil
	}

	next := ""
	if current.RefreshToken != refreshToken {
		next = current.RefreshToken
	}
	token, err = c.requestToken(ctx, next)
	// The new token replaces what the storage holds now, not the first
	// snapshot: another process may have stored a newer token meanwhile.
	return fetchResult{token: token, replaces: current}, err
}

// requestToken asks the shop for a token. Every failure is returned as a
// *TokenError.
func (c *Client) requestToken(ctx context.Context, refreshToken string) (tokenstorage.Token, error) {
	token, err := c.sendTokenRequest(ctx, refreshToken)
	if err != nil {
		return tokenstorage.Token{}, &TokenError{Err: err}
	}
	return token, nil
}

func (c *Client) sendTokenRequest(ctx context.Context, refreshToken string) (tokenstorage.Token, error) {
	fields, err := c.credentials.TokenRequest(ctx, refreshToken)
	if err != nil {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: build token request: %w", err)
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: encode token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/oauth/token", bytes.NewReader(payload))
	if err != nil {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: build token request: %w", err)
	}
	c.setCommonHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := c.readBody(resp)
	if err != nil {
		return tokenstorage.Token{}, err
	}
	if err = checkStatus(resp, body); err != nil {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: get token: %w", err)
	}
	return c.parseToken(resp, body, fields)
}

func (c *Client) parseToken(resp *http.Response, body []byte, fields map[string]string) (tokenstorage.Token, error) {
	var tr tokenResponse
	// The errors below never quote the body: a malformed token response can
	// still contain a valid access or refresh token, and errors end up in logs.
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: decode token response (status %d, content type %q, %d bytes): %w: %w",
			resp.StatusCode, resp.Header.Get("Content-Type"), len(body), ErrInvalidTokenResponse, err)
	}
	if tr.AccessToken == "" {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: token response (status %d, content type %q, %d bytes) has no access_token: %w",
			resp.StatusCode, resp.Header.Get("Content-Type"), len(body), ErrInvalidTokenResponse)
	}

	lifetime := time.Duration(tr.ExpiresIn) * time.Second
	if tr.ExpiresIn <= 0 {
		lifetime = defaultTokenLifetime
	}

	// Keep the used refresh token if the shop did not send a new one.
	refresh := tr.RefreshToken
	if refresh == "" && fields[fieldGrantType] == grantTypeRefreshToken {
		refresh = fields[fieldRefreshToken]
	}

	return tokenstorage.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: refresh,
		Expiry:       c.now().Add(lifetime),
	}, nil
}

// lockToken takes the lock for reading, checking and writing the stored
// token: the client's own lock, then the Locker if one is set. Without it, an
// invalidation could read a token, a refresh could store a new one (Shopware
// revokes the old refresh token), and the invalidation would then write back
// the revoked refresh token, losing the only valid one.
func (c *Client) lockToken(ctx context.Context) (func(), error) {
	select {
	case c.tokenMu <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-c.tokenMu }

	if c.locker == nil {
		return release, nil
	}
	unlock, err := c.locker.Lock(ctx, c.tokenKey)
	if err != nil {
		release()
		return nil, fmt.Errorf("adminapi: lock token: %w", err)
	}
	return func() {
		if unlock != nil {
			unlock()
		}
		release()
	}, nil
}

// invalidateUsedToken drops the access token after a 401, but only if it is
// still the one the failed request used. Otherwise another request already
// replaced it, which is not an error. The check and the write happen under
// the token lock.
func (c *Client) invalidateUsedToken(ctx context.Context, used string) error {
	unlock, err := c.lockToken(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	// A pending token was never handed out, so once it is stored the used
	// token no longer matches and nothing is invalidated.
	stored, err := c.loadReconciled(ctx)
	if err != nil {
		return err
	}
	if stored.AccessToken != used {
		return nil
	}
	return c.dropAccessToken(ctx, stored)
}

// dropAccessToken removes the access token of stored and keeps its refresh
// token. Without a refresh token nothing is left, so the token is deleted.
// The caller holds the token lock.
func (c *Client) dropAccessToken(ctx context.Context, stored tokenstorage.Token) error {
	if stored.RefreshToken == "" {
		return c.clearToken(ctx)
	}
	stored.AccessToken = ""
	stored.Expiry = time.Time{}
	return c.setToken(ctx, stored)
}

func (c *Client) loadToken(ctx context.Context) (tokenstorage.Token, error) {
	token, err := c.storage.Get(ctx, c.tokenKey)
	if errors.Is(err, tokenstorage.ErrNotFound) {
		return tokenstorage.Token{}, nil
	}
	if err != nil {
		return tokenstorage.Token{}, fmt.Errorf("adminapi: read token: %w", err)
	}
	return token, nil
}

func (c *Client) valid(token tokenstorage.Token) bool {
	return token.AccessToken != "" && c.now().Add(tokenExpiryMargin).Before(token.Expiry)
}

// isRejectedGrant reports whether the token endpoint rejected the grant
// itself, as it does for an expired or revoked refresh token. Only then may
// the client fall back to the credentials, for example the password. Other
// errors (invalid_client, a malformed request, a server error) are returned
// as they are, so the password is not sent for them.
func isRejectedGrant(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, e := range apiErr.Errors {
		switch e.Code {
		case errorInvalidGrant, shopwareInvalidRefreshToken, shopwareInvalidGrant:
			return true
		}
	}
	return false
}
