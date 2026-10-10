# Rails security checklist

Rails has more than fifteen years of security fixes. Cartridge does different things, but an app on it needs the same protection. This file compares the two, so the lessons of Rails become defaults, tests, or rules in cartridge.

Last full review: 10 October 2026, against Rails `main` and 220 advisories for the Rails gems and Rack (2006 to July 2026).

## How to keep it current

Do these four steps after each Rails security release, and before each cartridge minor release.

1. **Checklist.** For each Rails security feature, mark cartridge as *has*, *missing*, or *not applicable* in the first table.
2. **Port the tests.** For a feature that both have, copy the inputs of the Rails tests into `rails_parity_test.go`. The inputs are the value: each one broke Rails or Rack before.
3. **Follow the advisories.** For each new Rails or Rack advisory, find its class in the second table. Ask: does cartridge have this feature, and does this input break it? Add a test case when the answer is not obvious.
4. **Versioned defaults.** A new default that can break an app goes into `versionedDefaults` in `server.go`, under the next version. Apps get it with `WithDefaults("<version>")`. A default that cannot break an app goes into `DefaultServerConfig`.

A protection goes in the strongest place that can hold it: a default in code first, then a test, then a rule in `AGENTS.md` for what code cannot enforce.

## Features

| Rails | Cartridge | Status |
|---|---|---|
| CSRF protection (token, origin check; `Sec-Fetch-Site` in new versions) | `SecFetchSiteMiddleware`, on by default for POST, PUT, PATCH, DELETE | has |
| `HostAuthorization` | `ServerConfig.AllowedHosts`, with `.example.com` for subdomains | has, off until set |
| `RemoteIp` with trusted proxies | `ProxyHeader` and `TrustedProxies`. Stricter than Rails: no proxy is trusted by default, and `Client-IP` is never read | has |
| `redirect_to` refuses another host (`raise_on_open_redirects`) | `BlockExternalRedirects`, `RedirectLocal`, `RedirectBack`, `RedirectExternal` | has, with `WithDefaults("1.7")` |
| `load_defaults` | `ServerConfig.LoadDefaults`, `WithDefaults` | has |
| Strong parameters | `ctx.Bind` sets form fields only by an explicit `form` tag. A JSON body sets every exported field | partly: rule in `AGENTS.md` |
| Escaped templates (ERB, `SafeBuffer`) | `html/template` escapes by context | has |
| SQL placeholders in Active Record | GORM placeholders. GORM takes raw text in `First`, `Order`, `Select` | partly: rule in `AGENTS.md` |
| Default security headers | `SecurityHeaders`: `nosniff`, frame, referrer, and cross-origin policies | has |
| `force_ssl`: HSTS, secure cookies, redirect to https | HSTS and `Secure` cookies in production. No redirect from http | missing: the redirect |
| Content Security Policy with a nonce helper | `ContentSecurityPolicy` is sent when set. No default, no nonce helper | missing |
| `Permissions-Policy` | none | missing |
| Signed session cookie, `SameSite=Lax`, `HttpOnly` | the same | has |
| Encrypted session cookie | signed, not encrypted. It holds the user ID and two times | not needed today |
| Rotation of secrets for cookies and tokens | none. A new secret signs every user out | missing |
| `reset_session` at login | a login writes a new signed cookie; there is no server session to fix | not applicable |
| Ending sessions on the server | `WithSessionCheck` | has, off until set |
| `has_secure_password` (bcrypt) | `crypto.GeneratePasswordHash`, `crypto.VerifyPassword` | has |
| `authenticate_by` hides whether the user exists | none. The app compares by hand | missing |
| Signed tokens with a purpose and an expiry (`generates_token_for`, `signed_id`, `MessageVerifier`) | none. Each app writes its own reset and confirm tokens | missing |
| Active Record Encryption | `crypto.Encrypt` (AES-GCM). The key is padded, not derived, and cannot rotate | partly |
| `filter_parameters` in logs | request bodies are not logged, and the GORM logger drops values | has |
| `rate_limit` | `middleware.RateLimiter`, in memory, per process | has |
| Cache headers for private pages | `private, no-store` behind the session middleware | has |
| Cookie size check (`CookieOverflow`) | none. A browser drops a cookie over 4 KB without an error | missing |
| Body and parameter limits | `BodyLimit` (4 MB), read and write timeouts, the limits of Go's `net/http` and `mime/multipart` | has |
| `allow_browser` | none. A browser without `Sec-Fetch-Site` and `Origin` is refused on writes | not needed |
| HTML sanitizer for rich text | none. Cartridge has no rich text | not applicable |
| Active Storage (uploads, variants, direct upload) | none | not applicable; read the class below before adding uploads |
| Development is local (`web-console`, host check in development) | development and test bind `127.0.0.1` only | has |
| Dependency and code scan in CI (`bundler-audit`, Brakeman) | `govulncheck` in CI. No static security scan | partly |

## Advisory classes

The classes of the 220 advisories, the most common first. About 40 old advisories have a summary that names no class, so the order is approximate.

| Class | What went wrong in Rails or Rack | Cartridge |
|---|---|---|
| XSS in view helpers, the sanitizer, and `SafeBuffer` | A helper marked text as safe HTML that held request data | `html/template` escapes by context. Cartridge's own helpers that return `template.HTML` (`render`, `embed`, `importmap`) write escaped or JSON-encoded text. Rule: do not use `template.HTML` on a request value |
| DoS and ReDoS in header, query, and multipart parsing | A regular expression or a parser took too long or too much memory | Go's `regexp` runs in linear time, and `net/http` and `mime/multipart` have limits. `BodyLimit` bounds the body before parsing |
| SQL injection through a query method argument | Text from a request reached a method that takes SQL | The same risk in GORM. Rule in `AGENTS.md`. No code check |
| Path traversal and file disclosure (static files, `render file:`, `send_file`) | An encoded path left the folder, or a template name came from a request | Static files: `TestStaticFilesStayInTheirFolder`. Rules for `SendFile` and `Render` |
| Uploads (Active Storage) | Content type from the client, path from the client, range requests | Not applicable. No upload support |
| Unsafe deserialization and code injection (YAML, Marshal, `render inline:`) | Request data reached a deserializer or a template compiler | Cartridge decodes JSON and forms only. The cache reads `gob` from its own table. Templates are parsed from files only |
| Session and cookie handling (fixation, a deleted session that returns, cookie prefix overwrite) | Server session state was reused | Stateless signed cookie. Old sessions end only with `WithSessionCheck` |
| Host header and forwarded headers | A crafted `Host` passed the allowlist by its end; `X-Forwarded-Host` and `Forwarded` were trusted | `TestRailsHostCases`. Cartridge never reads `X-Forwarded-Host` or `Forwarded` |
| Open redirect | A location with `//`, a backslash, or leading white space left the site | `TestRailsRedirectCases` |
| CSRF bypass | A token could be forged or reused | No tokens. `TestRailsCSRFCases` |
| Response splitting and header injection | A line break in a header value | `net/http` removes line breaks from header values: `TestRailsRedirectCases` |
| Log injection (ANSI escapes, forged lines) | A request value was written raw to the log | Production logs are JSON. The development log escapes control characters: `TestDevLogEscapesControlCharacters` |
| Information leak in error pages | A debug page showed internals in production | Production error pages show the status text only. Development binds loopback |
| Timing attacks | A secret was compared byte by byte | `hmac.Equal` for the session. bcrypt for passwords |
| Mass assignment | A parameter set a protected attribute | See "Strong parameters" above |

## Ported tests

| Rails test file | Cartridge test | Cases |
|---|---|---|
| `actionpack/test/dispatch/host_authorization_test.rb` | `TestRailsHostCases` | 23 |
| `actionpack/test/dispatch/request_test.rb` (remote IP) | `TestRailsClientIPCases`, and `TestTrustedProxies` from before | 9 |
| `actionpack/test/controller/redirect_test.rb` | `TestRailsRedirectCases` | 13 locations, 6 behaviours |
| `actionpack/test/controller/request_forgery_protection_test.rb` | `TestRailsCSRFCases`, and `sec_fetch_test.go` from before | 6 behaviours |
| Rack::Static and Rack::Directory advisories | `TestStaticFilesStayInTheirFolder` | 13 paths |
| Rack::CommonLogger and Active Record logging advisories | `TestDevLogEscapesControlCharacters` | 1 |

Not ported, because the feature differs:

- Rails picks the client IP from `X-Forwarded-For` for any peer, and reads `Client-IP`. Cartridge reads the header only from a trusted proxy.
- Rails cookie tests cover the cookie jar (encrypted cookies, rotation, overflow). Cartridge has none of these yet.
- Rails CSRF tests cover tokens. Cartridge uses `Sec-Fetch-Site` and `Origin`.

## Missing, by value

1. Signed tokens with a purpose and an expiry, for password reset, email confirm, and signed IDs.
2. Rotation of the session secret, so a new secret does not sign every user out.
3. A default Content Security Policy with a nonce helper, as a versioned default.
4. A redirect from http to https in production, as a versioned default.
5. A password check that takes the same time for an unknown user.
6. A real key derivation and rotation for `crypto.Encrypt`.
7. A cookie size check in `SetCookie`.
8. `Permissions-Policy`.
