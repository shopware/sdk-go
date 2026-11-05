import { location, context } from '@shopware-ag/meteor-admin-sdk'

export interface ShopwareContext {
  shopId: string
  shopUrl: string
  locale: string
  accessToken: string
}

/**
 * How long before expiry the token is refreshed proactively. With the backend's
 * 15-minute token this fires around minute 12, so a tab that sits idle keeps its
 * session alive instead of dying on the backend's 20-minute renewal window.
 */
const REFRESH_LEAD_MS = 3 * 60 * 1000

/** Never schedule a refresh closer than this, to avoid a tight timer loop. */
const MIN_REFRESH_DELAY_MS = 5 * 1000

/** A transient failure backs off from this delay, doubling up to the cap. */
const RETRY_BASE_MS = 5 * 1000
const RETRY_MAX_MS = 60 * 1000

/**
 * How many transient failures to ride out before giving up. Eight attempts —
 * 5s, 10s, 20s, 40s, then 60s each — span about five minutes, so the last one
 * still lands inside the backend's renewal grace.
 */
const MAX_REFRESH_RETRIES = 8

const backendUrl = (): string =>
  import.meta.env.VITE_API_URL || 'http://host.docker.internal:8085'

let shopwareContext: ShopwareContext | null = null

// Session state kept out of ShopwareContext: callers only ever need the token.
let tokenExpiresAt = 0
let refreshTimer: ReturnType<typeof setTimeout> | null = null
let inFlightRefresh: Promise<string> | null = null
let refreshAttempt = 0
let sessionExpired = false
const sessionExpiredListeners = new Set<() => void>()

/**
 * Initialize Shopware context from Meteor Admin SDK
 * This gets shop information and authentication details from the parent Shopware admin
 */
export async function initializeShopwareContext(): Promise<ShopwareContext> {
  try {
    // Get shop ID from Shopware Admin using v6 API
    const shopId = await context.getShopId()
    
    if (!shopId) {
      throw new Error('Shop ID not available in context')
    }

    // Get locale
    const localeData = await context.getLocale()

    // Build the authentication URL with Shopware-provided context
    // IMPORTANT: We must preserve the EXACT query string that Shopware signed
    // We cannot reconstruct it because parameter order matters for the signature!
    //
    // This is also the only moment the signed query is usable: the SDK expires
    // it after a few minutes, so from here on the token is what we renew.
    const originalQuery = window.location.search

    // Call our backend with the same query string Shopware sent
    const authUrl = `${backendUrl()}/api/auth/token${originalQuery}`

    // Get access token from our backend
    const response = await fetch(authUrl.toString(), {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json',
      },
    })

    if (!response.ok) {
      const error = await response.json()
      throw new Error(error.error || 'Failed to authenticate')
    }

    const authData = await response.json()

    // Store context
    shopwareContext = {
      shopId,
      shopUrl: window.location.origin,
      locale: localeData.locale || 'en-GB',
      accessToken: authData.token,
    }

    sessionExpired = false
    noteTokenLifetime(authData.expiresIn)

    console.log('Shopware context initialized:', {
      shopId: shopwareContext.shopId,
      locale: shopwareContext.locale,
    })

    return shopwareContext
  } catch (error) {
    console.error('Failed to initialize Shopware context:', error)
    throw error
  }
}

/**
 * Get the current Shopware context
 */
export function getShopwareContext(): ShopwareContext {
  if (!shopwareContext) {
    throw new Error('Shopware context not initialized')
  }
  return shopwareContext
}

/**
 * The current token, or null before the context is initialized. Unlike
 * getShopwareContext() this does not throw, so a failure path can compare
 * tokens without a try/catch.
 */
export function currentAccessToken(): string | null {
  return shopwareContext?.accessToken ?? null
}

/**
 * Refresh the access token.
 *
 * The signed query in the iframe URL cannot be replayed: the SDK expires signed
 * queries after a few minutes, so the current token is the credential used to
 * obtain the next one. The backend slides the session up to an absolute cap;
 * once that is reached the module has to be reopened to get a freshly signed URL.
 *
 * Concurrent callers share one request: the proactive timer and a 401 retry can
 * fire together, and two refreshes would rotate the token twice for nothing.
 */
export function refreshAccessToken(): Promise<string> {
  if (inFlightRefresh) {
    return inFlightRefresh
  }

  inFlightRefresh = performRefresh().finally(() => {
    inFlightRefresh = null
  })

  return inFlightRefresh
}

async function performRefresh(): Promise<string> {
  // Hold on to the context we started with: the token is written back after an
  // await, and re-reading the module-level binding there would be ambiguous.
  const ctx = shopwareContext
  if (!ctx) {
    throw new Error('Shopware context not initialized')
  }

  const response = await fetch(`${backendUrl()}/api/auth/refresh`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${ctx.accessToken}`,
    },
  })

  if (!response.ok) {
    // 401/403 mean the session is over or the shop was deactivated: nothing short
    // of a freshly signed URL helps, so end the session. Anything else — a 5xx, a
    // proxy error while the backend is redeploying — says nothing about the
    // session and must not kill one that is still valid.
    if (response.status === 401 || response.status === 403) {
      markSessionExpired()
      throw new Error('Session expired, reload the module to continue')
    }

    throw new Error(`Token refresh failed with status ${response.status}`)
  }

  const { token, expiresIn } = await response.json()
  ctx.accessToken = token
  noteTokenLifetime(expiresIn)

  return token
}

/**
 * Register a listener invoked once the session can no longer be renewed. The
 * proactive refresh runs in the background, so without this the UI would never
 * learn that the session ended.
 */
export function onSessionExpired(listener: () => void): () => void {
  sessionExpiredListeners.add(listener)
  return () => sessionExpiredListeners.delete(listener)
}

export function isSessionExpired(): boolean {
  return sessionExpired
}

/** Stop the proactive refresh, e.g. when tearing the module down. */
export function stopTokenRefresh(): void {
  if (refreshTimer !== null) {
    clearTimeout(refreshTimer)
    refreshTimer = null
  }
}

function markSessionExpired(): void {
  stopTokenRefresh()
  if (sessionExpired) {
    return
  }

  sessionExpired = true
  sessionExpiredListeners.forEach((listener) => listener())
}

/** Record the new token lifetime and schedule the next proactive refresh. */
function noteTokenLifetime(expiresInSeconds: unknown): void {
  // A refresh got through, so the backoff starts over.
  refreshAttempt = 0

  const lifetimeMs = typeof expiresInSeconds === 'number' ? expiresInSeconds * 1000 : 0

  // Without a usable expiry we cannot schedule anything; the 401 retry in
  // api.ts stays as the fallback.
  if (lifetimeMs <= 0) {
    tokenExpiresAt = 0
    stopTokenRefresh()
    return
  }

  tokenExpiresAt = Date.now() + lifetimeMs

  if (lifetimeMs > REFRESH_LEAD_MS) {
    scheduleRefresh(lifetimeMs - REFRESH_LEAD_MS)
    return
  }

  // The backend clamps a token to the end of the session, so a lifetime shorter
  // than the lead time means we are at the session cap. Refreshing on the lead
  // schedule would then poll every few seconds for nothing. Make exactly one
  // attempt just after expiry: inside the backend's grace window it renews, and
  // at the cap it fails and surfaces the ended session to the UI.
  scheduleRefresh(Math.max(lifetimeMs + MIN_REFRESH_DELAY_MS, MIN_REFRESH_DELAY_MS))
}

function scheduleRefresh(delayMs: number): void {
  stopTokenRefresh()

  refreshTimer = setTimeout(() => {
    refreshTimer = null
    void refreshAccessToken().catch((error) => {
      console.error('Proactive token refresh failed', error)
      retryAfterTransientFailure()
    })
  }, delayMs)
}

/**
 * Keep the proactive loop alive after a failure that says nothing about the
 * session: a network blip, a 5xx, a malformed body. Without it a single failed
 * attempt ends the loop for good, and an idle tab then sits there until its
 * token expires past the renewal grace.
 */
function retryAfterTransientFailure(): void {
  // A fatal failure already called markSessionExpired(), which stopped the loop.
  if (sessionExpired) {
    return
  }

  if (refreshAttempt >= MAX_REFRESH_RETRIES) {
    console.error('Giving up on proactive token refresh; the 401 retry in api.ts remains')
    return
  }

  const delay = Math.min(RETRY_BASE_MS * 2 ** refreshAttempt, RETRY_MAX_MS)
  refreshAttempt += 1
  scheduleRefresh(delay)
}

// Browsers throttle timers in background tabs, so a refresh scheduled while
// hidden can fire late. Re-check on the way back in and renew immediately if the
// token is already inside its refresh window.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible' || sessionExpired || !shopwareContext) {
      return
    }

    if (tokenExpiresAt === 0 || Date.now() < tokenExpiresAt - REFRESH_LEAD_MS) {
      return
    }

    void refreshAccessToken().catch((error) => {
      console.error('Token refresh after tab became visible failed', error)
      retryAfterTransientFailure()
    })
  })
}

/**
 * Update the iframe height based on content
 */
export function updateIframeHeight(height: number): void {
  location.updateHeight({
    height,
  })
}
