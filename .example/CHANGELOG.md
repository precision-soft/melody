# Changelog

All notable changes to the example application of `precision-soft/melody` will be documented in this file.

The example application is not a published module — `go get` never resolves `precision-soft/melody/.example` — so it carries no version of its own. It ships inside the major it demonstrates and is released on THAT major's tags: every block below names the same version, date and title as the block of the same release in [`../CHANGELOG.md`](../CHANGELOG.md), and the compare links point at the same two tags.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- example: logging out ends the session instead of emptying it. Both doors — the page redirect and the firewall's logout handler — deleted the two identity keys, which leaves the entry MODIFIED, so the response path saved it back under the same id and re-issued the live cookie: measured, the storage still held the record after the response ran, and the browser carried a session id across its own logout, for the whole session lifetime. `Clear()` is what marks a session cleared, and a cleared session is what routes the response path to `DeleteSession` and to the expired cookie
- example: an admin update that names no `roles` keeps the ones the target holds, instead of stripping it to the base role. An omitted username and an omitted password were already kept; roles were the one field an omission REMOVED, an administrator editing their own account included, and this door is the only one that can grant them back. A list sent explicitly empty still falls back to the base role, which is the rule `normalizeRoles` carries — the decoder separates "absent" from "empty", so the two readings never had to be fused

## [v1.19.0] - 2026-08-18 - Stabilization Sweep, Hardened Failure Paths and Feature Freeze

### Added

- example: the example carries the source of its own frontend bundle, in `.example/assets/` — `app.ts`, the `melody-routes.ts` URL generator, and the `package.json` that bundles them into `public/assets/app.js` with esbuild.
- example: a stateless api-key firewall on `/products/api`, which is the door `APP_API_TOKEN` always promised — the key shipped in `.env`, was marked secret, and nothing read it.
- example: the cors LISTENERS, armed by `APP_CORS_ALLOW_ORIGINS` (comma separated; empty keeps cors unwired): a preflight aimed at an access-controlled path is answered 204 before routing and before the security chain can refuse it, and the refusals the security listeners produce carry the cors headers — responses the middleware chain never sees, which is why the listeners are the door the example demonstrates rather than the middleware.
- example: file-backed session storage as a configuration choice — `APP_SESSION_FILE` names the snapshot (a relative path is anchored to the project directory) and the example registers `session.NewFileStorageFromPath` under the framework's storage service id, which wins over the has-guarded in-memory default; empty keeps the default.
- example: a second live database in the same process — the catalog journal moves onto `bunorm/pgsql` while the catalogue stays on mysql, which is what shows the provider is a choice rather than an assumption.
- example: the request-scoped change attribution, `service.ChangeAttribution` — the example's own demonstration of `RegisterScopedServices` and of `container.Lazy`.
- example: the development stack serves all three example applications at once, each under a name that says which it is — `v1-example.`, `v2-example.` and `example.melody.localhost.precision-soft.com`.
- example: the example application is a working nomenclature rather than a set of routes that exist to be driven.
- example: every redis key and every table the example writes carries its major.

### Changed

- example: each major's example application holds its schema in a database of its own rather than the one all three shared.
- example: the catalog migration set holds four migrations — the journal's `20260814000005` moves, with its name, into the postgres-dialect `JournalMigrations` set (see the Added entry), so `db:migrate` over an emptied catalog database reports `applied 4 migrations`. **Behavioural change**. **Operational note**
- example: the static cache is armed in the shipped `.env` — `MELODY_STATIC_ENABLE_CACHE=true`, `MELODY_STATIC_CACHE_MAX_AGE=3600` — where every example previously opted out of the framework's own default.
- example: the seeded passwords are bcrypt. **Operational note**
- example: a validation refusal answers one `errors` entry per violated field — `presenter.ApiValidationError`, which both product write handlers now answer through — instead of the single semicolon-joined, alphabetically sorted string the presenter used to receive from `ValidationErrors.Error()`. **Behavioural change**
- example: the four example commands — `app:info`, `product:list`, `catalog:journal`, `catalog:report:refresh` — render through the framework's `cli/output` envelope instead of `fmt`, so each accepts the standard flag set and answers one machine-readable json document under `--format=json`. **Behavioural change**
- example: the cron wiring moves onto the module facade — `configure.go` registers `cron.NewModule` with the configuration factory and the three scheduled commands, instantiated, as its runner commands, and the hand-wired registration of the generate command is gone.
- example: the database schema is owned by a migration set, `migration/` — five DDL migrations whose column definitions were captured from the tables the repositories used to create — and the repository-owned `EnsureSchema` is gone from the five bun repositories and from the journal repository's public interface.
- example: the catalogue reading is served at `/catalog/report/` under the name `example.catalog.report`.
- example: the welcome text on the static index says what the application is — a product nomenclature of products, categories, currencies and users — instead of calling itself a small demo, and the api token shipped in `.env` is named for the example rather than for a demonstration.
- example: the api presenter answers `406 Not Acceptable` when the accept header refuses every available media type, exactly as the framework result handler answers the same header on the success path. **Behavioural change**

### Fixed

- the example catches up with the identities its own cache promises, on the doors a divergence was measured through: the bun user lookup compares on the binary collation (`LOWER(username) = (? COLLATE utf8mb4_bin)`), because the column's accent-insensitive default (`'café' = 'cafe'` is true under `utf8mb4_0900_ai_ci`) admitted spellings the cache keys and the invalidation listeners — which fold with `NormalizedUsername` alone — could never address, so a deleted user kept authenticating from the ttl-less cache under the collation-only spelling; `UserUpdatedEvent` carries the username the row held before the update and the listener drops both spellings, since a rename left the entry behind under the old one; caller-supplied identifiers are answered as absent by every finder when the cache-key grammar refuses them (a space, a newline, over 255 bytes) instead of surfacing the backend's refusal as a 500 on a read, and the write doors refuse the same spellings as a 400 that names the field —
  a product id with an interior space used to land in the database and then fail every later cache write, with the created row invisible to the ttl-less list forever; the invalidation listeners run every delete and join the failures instead of returning on the first, which used to skip the list entry behind it; the login door no longer concatenates the failure's internals into the client response; the password doors refuse the bcrypt 72-byte ceiling as a 400 instead of a 500, a role carrying a comma is refused before the comma-joined storage would split it into roles nobody granted on the next read, and the embedded-env build embeds the committed `.env` alone — the `.env*` glob also baked the gitignored `.env.local`, the machine-local file that holds real credentials precisely because it never enters git, into the shipped binary, where the loader's precedence let it override the committed configuration.
- example: the login flow rotates the session id before writing the authenticated identity, the framework's own defence against session fixation (`http.RegenerateRequestSession`) that the showcase demonstrated unused — the identity was written onto the pre-login session, so an id an attacker seeded and planted in the victim's browser stayed authenticated as the victim.
- example: the login entry point and the access denied handler read the client's preference through `melodyhttp.PrefersHtml`, which is what the rest of the example already used.
- example: `CacheKeyUserByUsername` folds the username itself instead of trusting its callers to have folded it.
- example: an administrator may not modify or delete a peer, and both doors ask the same question.
- example: the seeded password digest is compared in constant time with `crypto/subtle.ConstantTimeCompare` rather than with `!=`.
- example: `/health` stamps its answer with the injected clock rather than with `time.Now`.
- example: the README's structure overview lists `assets/`, the frontend bundle source that produces `public/assets/app.js` and is tracked in git, and no longer describes `repository/` as carrying only in-memory implementations or `service/` as carrying four of its six services.
- example: `/health` answers a monitoring probe in all three examples, where v1 and v2 left it to the `ROLE_USER` catch-all and v3 alone had made it public.
- example: `/index.html` carries the same public policy as the `/` it serves.
- example: the README of every major describes the cron wiring that exists.
- example: `url/url_generator.go` is deleted from all three examples.
- example: the shared icons every page links — `favicon.ico`, `assets/favicon.svg`, `assets/logo.png`, `assets/apple-touch-icon.png` — are produced by the same `npm run build` that produces the frontend bundle, so the one command a fresh clone needs for the browser interface delivers everything a browser asks for.
- example: the comment in `.env` no longer claims that an already-set process or host environment variable overrides the value beside it.
- example: the route manifest is escaped for the javascript string literal it is spliced into, so a route name or pattern containing a backslash or an apostrophe no longer breaks every page's scripting (the escaping v3 already carried); the firewall session login handler stores the token roles alongside the user identifier, and the logout handler clears them, so a session written by that handler resolves back to an authenticated token rather than an anonymous one; the embedded static build embeds dot-prefixed and underscore-prefixed paths (`all:public`), so it serves the same file set as the filesystem build
- example: the in-memory repositories guard their slice with a read-write mutex, and `All` hands back a copy of it.
- example: the api error presenter emits the raw error message, the concrete Go type and the unwrap chain only when the kernel environment is the development one, the same gate the framework exception listener applies, and stays closed when that environment cannot be resolved at all.

[Unreleased]: https://github.com/precision-soft/melody/compare/v1.19.0...HEAD

[v1.19.0]: https://github.com/precision-soft/melody/compare/v1.18.1...v1.19.0
