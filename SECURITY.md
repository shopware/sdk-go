# Security Policy

## Supported Versions

Security fixes are released for the latest minor version of each module
(`github.com/shopware/sdk-go`, `integrations/gin`, `clients/adminapi`). Upgrade to the
latest release to receive them. Older lines are supported only when the maintainers
announce it in the release notes.

## Go Toolchain

Build and run the SDK with an up-to-date Go patch release: **Go 1.26.10+** (or
the latest 1.27.x, 1.27.3+). Standard-library advisories reported by `govulncheck ./...`
are toolchain issues — they are fixed by installing a patched Go release, never
by upgrading a module dependency. CI pins the exact patch via `GO_VERSION` in
`.github/workflows/ci.yml` and fails on any called vulnerability.

## Replay Protection

An HMAC proves *who* built a request; it says nothing about *when*. A signed
request that leaks — browser history, `Referer`, proxy and access logs,
analytics, a copied link — replays forever unless the app parses the signed
`timestamp` and compares it with the current time.

The SDK therefore applies a freshness policy on top of every signature. Because
Shopware does not stamp every payload, the default differs per path — and each
default is enforced by the parity tests in `resolver/policy_test.go`:

| Path | Timestamp | Active shop | Policy |
| --- | --- | --- | --- |
| Signed GET query | required | enforced | `resolver.QueryPolicy` |
| Webhook | required | enforced | `webhook.Policy` |
| Signed JSON POST | checked if present | enforced | `resolver.PostPolicy` |
| Lifecycle webhook | required | not checked (by design) | `resolver.LifecyclePolicy` |

Requiring a timestamp on webhooks is safe: Shopware's
`AppPayloadServiceHelper::createWebhookRequest` stamps one unconditionally and
re-stamps and re-signs the body on every delivery attempt, so its retry ladder
(5s, 30s, 5m, 30m, 4h) always arrives inside the window.

Signed POSTs cannot be strict, because none of them carry a timestamp in the
`timestamp` field the SDK reads. Payment, tax provider, checkout and context
gateway payloads are built by `createRequestOptions`, which sets only `source`.
Action buttons take a different path — `ActionButton\Executor` stamps
`meta.timestamp` — but the SDK does not look there, so the effect is the same.
Requiring a timestamp would reject all of them, so this path bounds replay only
for webhook-shaped bodies; anything else needs a nonce, a dedup on
`source.eventId`, or endpoint authorization. Reading `meta.timestamp` would close
the action-button gap and is tracked separately.

Lifecycle routes (`activate`, `deactivate`, `delete`) deliberately serve inactive
shops — that is the whole reason they have a resolver method of their own — but
they are stamped like any other webhook, because the manifest registers them as
webhooks. So they get the same required timestamp, plus a check no other path
needs: the signed event must match the route.

### Lifecycle routes must be bound to their event

A signature answers *which shop secret signed these bytes*. It never answers
*which state transition those bytes authorize*. On every other path the two
questions collapse into one, because the request carries only one possible
meaning. Lifecycle is different: three routes perform three different mutations
against the same shop, and the route is chosen by whoever sends the request.

Left unbound, that gap is directly exploitable. A correctly signed
`app.activated` body POSTed to `/app/delete` deletes the stored shop, because the
handler takes its transition from the URL and reads the signed event only for the
log line. Freshness alone does not close it either — inside the window, a live
activate request is a delete request waiting to be re-aimed.

`Resolver.ResolveLifecycleWebhook` therefore takes the event the route accepts,
and rejects anything else with `403 event_mismatch`:

```go
shop, event, err := sdk.Resolver().ResolveLifecycleWebhook(
    ctx, req, webhook.EventAppDeleted,
)
```

The argument is mandatory and the check has no opt-out — it is not a field on
`resolver.LifecyclePolicy`, which governs freshness only. Shopware never sends
one lifecycle event to another's route, so an `event_mismatch` in production is a
security signal, not a compatibility problem: treat it as possible exposure of a
shop secret or of captured signed traffic.

### Signed queries in detail

Browser-initiated requests (admin modules, action button tabs and modals, custom
signed links) are the highest-risk path, because the credential sits in a URL.
`resolver.QueryPolicy` governs them, and its zero value is the strictest policy
and the default:

| Check | Default |
| --- | --- |
| `timestamp` present | Required (`TimestampRequire`) |
| Maximum age | 5 minutes (`signature.DefaultReplayWindow`) |
| Future tolerance | 1 minute (`signature.DefaultClockSkew`) |
| Inactive shops | Rejected (403 `shop_not_active`) |

Configure it once through `app.Config.QueryPolicy` (or `Config.QueryPolicy` in
the Gin integration), and override it per route with
`Resolver.ResolveFromQueryWithPolicy` / `GetRequestVerifierWithPolicy`.

Note that this is **stricter than the official Shopware SDKs**: neither
`app-php-sdk` (`RequestVerifier::authenticateGetRequest`) nor `app-sdk-js`
(`verifyGetRequest`) validates the timestamp, and neither enforces shop state on
signed queries. All documented Shopware signed-query flows do send a timestamp,
so the strict default is protocol-compatible. If you own a flow that legitimately
omits it, opt out explicitly per route rather than globally:

```go
router.GET("/legacy", ginintegration.GetRequestVerifierWithPolicy(sdkApp, logger,
    resolver.QueryPolicy{Timestamp: resolver.TimestampVerifyIfPresent}), handler)
```

Freshness limits the replay window; it does not make a signed URL single-use. For
endpoints that mint a session, issue a bearer token, or change state, keep the
downstream credential short-lived and authorize it independently — see
`examples/admin-extension`, where the signed query buys a 15-minute token rather
than a 24-hour one.

## Re-Registration Requires The Current Shop Secret

Re-registering an already confirmed shop requires a valid `shopware-shop-signature`,
and confirming that re-registration requires a valid `shopware-shop-signature-previous`.
Both checks fail closed: a missing or invalid signature is a `401`.

This is mandatory, not a hardening option. The app secret lives in `manifest.xml` and
identifies the app, not the shop, so anyone holding it could otherwise re-register a
victim shop, receive a fresh pending secret, confirm the rotation with that secret alone,
and end up owning the shop secret while the stored shop URL and API credentials are
replaced. Shopware tracks this as
[GHSA-c4p7-rwrg-pf6p](https://github.com/shopware/shopware/security/advisories/GHSA-c4p7-rwrg-pf6p)
(CVSS 8.9) and requires apps to validate both signatures.

| Path | Requirement |
| --- | --- |
| First registration of an unknown shop | app signature only — no prior secret exists |
| Re-registration, shop not yet confirmed | app signature only |
| Re-registration, shop confirmed | app signature **and** shop signature |
| Confirmation, first registration | pending-secret signature |
| Confirmation, shop confirmed | pending-secret signature **and** previous-secret signature |

`app.Config.AllowUnsignedReregistration` waives the requirement, and does so only for
shops that have never proved knowledge of their secret. It exists for installations still
on Shopware older than 6.6.10.15 / 6.7.8.1, which do not send the shop signature at all.
Enabling it restores a known-vulnerable path.

Accepted residual risk: the first registration of a shop the SDK has never seen is
authenticated by the app signature alone. That is a property of the handshake — there is
no shared per-shop secret yet — and cannot be fixed inside the SDK.

## Reporting A Vulnerability

Do not disclose security issues in public GitHub issues.

Use Shopware's coordinated vulnerability reporting process:

- Security reporting page: https://www.shopware.com/en/contact/security-reporting
- Security email: security@shopware.com

Include:

- Affected package, version, and commit if known.
- Reproduction steps or proof of concept.
- Impact assessment.
- Any known workarounds.

## Security-Relevant SDK Areas

- Registration signature verification.
- Confirmation request verification.
- Webhook and lifecycle signature verification.
- App secret rotation and previous-secret fallback.
- Token acquisition and token storage.
- URL construction for registration confirmation.

Changes in these areas need focused tests and should avoid weakening constant-time comparisons, timestamp validation, or request body integrity.
