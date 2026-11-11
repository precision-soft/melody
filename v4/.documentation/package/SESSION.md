# SESSION

The [`session`](../../session) package provides a small session subsystem for Melody: session ids, in-memory storage, and a manager that persists modified sessions.

## Scope

- Package: [`session/`](../../session)
- Subpackage: [`session/contract/`](../../session/contract)

## Subpackages

- [`session/contract`](../../session/contract)\
  Public contracts (`Manager`, `Storage`, `Session`).

## Responsibilities

- Create and load sessions through a `Manager` (`NewSession`, `Session`).
- Rotate a session id without losing its values (`RegenerateSession`), the defence against session fixation.
- Provide an in-memory `Storage` implementation for development/testing.
- Persist session changes only when a session is modified (and delete when cleared).
- Provide container helpers to resolve the session manager and storage.

## Container integration

The package defines the service ids:

- [`ServiceSessionManager`](../../session/service_resolver.go) (`"service.session.manager"`)
- [`ServiceSessionStorage`](../../session/service_resolver.go) (`"service.session.storage"`)

Resolution helpers:

- [`SessionMustFromContainer`](../../session/service_resolver.go)
- [`SessionStorageMustFromContainer`](../../session/service_resolver.go)
- [`SessionStorageMustFromResolver`](../../session/service_resolver.go)

## Usage

The example below demonstrates resolving the session manager from the container and persisting a modified session.

```go
package main

import (
    "context"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/session"
)

func updateSession(
    ctx context.Context,
    serviceContainer containercontract.Container,
    sessionId string,
) error {
    manager := session.SessionMustFromContainer(ctx, serviceContainer)

    sessionInstance, loadErr := manager.Session(sessionId)
    if nil != loadErr {
        return loadErr
    }

    if nil == sessionInstance {
        sessionInstance, loadErr = manager.NewSession()
        if nil != loadErr {
            return loadErr
        }
    }

    sessionInstance.Set("userId", "u-123")

    return manager.SaveSession(sessionInstance)
}
```

## Session id validation

Session ids are always 32-character lowercase hex strings. Incoming cookies that do not match this shape are rejected by [`Manager.Session`](../../session/manager.go) without touching storage, and [`DeleteSession`](../../session/manager.go) refuses malformed ids. New ids are generated from 16 bytes of `crypto/rand`.

## File storage durability

[`NewFileStorage`](../../session/file_storage.go) over a `Path` flushes modifications atomically via `os.CreateTemp` + `os.Rename`, and fsyncs the parent directory after the rename, so a crash mid-write cannot leave a truncated session file on disk and a power loss right after a save cannot resurface the previous snapshot. A hard kill between the temp file and the rename leaves an orphan `<name>.<random>.tmp` beside the store — a complete snapshot of every live session — and the next construction sweeps its own temp spelling away before reading. File mode `0755` is used for the parent directory.

## Rotating the session id

[`Manager.RegenerateSession`](../../session/manager.go) is the defence against **session fixation**: it mints a fresh id, carries the current values over to it, and removes the storage entry the previous id pointed at. Rotate on any privilege change — above all at login, so an id an attacker managed to plant in the victim's browser before authentication is not the id that ends up carrying the authenticated identity.

Rotation lives on the `Manager` rather than on the `Session` because only the manager holds the storage the candidate id is probed against and the previous entry is deleted from; a `Session` keeps no storage reference.

A firewall's [sign-in door](SECURITY.md#sign-in-and-sign-out-doors) with an `Authenticator` rotates before its handler runs and hands it `LoginInput.PreviousSessionId`; a handler behind a door without one rotates itself. Place the rotation **before** the handler writes the authenticated identity, and reach for [`http.RegenerateRequestSession`](../../http/session.go) rather than the manager: it rotates and republishes in one call, so the two halves cannot be half-done.

```go
rotated, rotateErr := melodyhttp.RegenerateRequestSession(request)
if nil != rotateErr {
    return nil, rotateErr
}

/* sessionKeyUserId is application-owned; the framework defines no session key for the identity */
rotated.Set(sessionKeyUserId, user.Id())
```

The returned session is a **new object marked modified**, and the one passed in is latched out of use. Publishing the new object on the request — which the helper does for you — is what makes the response path save it and emit its cookie. `Manager.RegenerateSession` stays the storage-level primitive underneath, for code that holds no request.

## Footguns & caveats

- `Manager.SaveSession` only persists when `Session.IsModified()` is true; a read-only session is not written.
- Clearing a session (`Session.Clear()`) ends it, and the ending **latches**: saving a cleared session deletes it, and a later `Set` puts the value back and marks the session modified but cannot make it look live again. That is what stops a logout followed by a farewell flash message — written by a middleware or an event listener to the same object — from taking the save branch and re-issuing the pre-logout id. A caller that wants a usable session after clearing one asks the manager for a new session.
- `Session.All()` returns a copy that descends maps and slices, the depth both storages copy at, as `Get` and `Set` do; a pointer, a struct holding a map, a channel or a func inside it is kept shared, so a caller who stores such a value owns its synchronisation. Mutating the returned map, or anything nested inside it, does not touch the live session — which also means such a mutation is not persisted, because it never passed through `Set`.
- A session another request deleted cannot be saved again — within one process. `Manager.DeleteSession` remembers the id for five minutes by default — the window is configurable through `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` or `ManagerConfig.TombstoneRetention` of [`session.NewManager`](../../session/manager.go), at least one second, unlimited refused — and `SaveSession` refuses a write under it, returning an error whose cause is [`session.ErrSessionDeleted`](../../session/manager.go); a rotated-away id is remembered the same way. Without it a request that loaded the session before a logout deleted it would re-create the entry when its own handler finished. The response path answers that refusal by expiring the browser cookie and serving the handler's response unchanged — it is the session ending, not a storage failure. The record lives in the manager's own memory, not in the storage it guards: a shared storage does not carry it between instances, so on a deployment with more than one
  node a logout served by one instance does not stop a peer instance's in-flight request from writing the entry back when its handler finishes. The record is bounded by the window, not by how many sessions were ever deleted.
- `RegenerateSession` asks the same record, in the same critical section as its removal: a copy of the session loaded before a logout or before an earlier rotation is refused with the error `SaveSession` answers for it (`ErrSessionDeleted`, with `ErrSessionRotated` beside it for a rotated-away id), before anything is stored, so it can neither carry the pre-logout identity onto a fresh id nor fork one session into two. The record holds every id a delete leaves absent: the one whose entry lapsed or was evicted while a request held a copy of it is buried too, so that copy cannot write the ended session back; a delete the storage reports as failed is remembered only when a fresh read says the entry is gone — a storage that removed the entry and then failed to flush it — so a failed logout never locks out a session that is still stored. The response path deletes only a session the request's cookie named, so a logout presenting an unknown cookie, for which the kernel minted a fresh session,
  leaves no record; and the rotation of a session the manager minted and that was never saved — a sign-in on a fresh session — removes nothing and leaves no record, nor does the save of such a session once cleared, the original that rotation left cleared included. A direct `DeleteSession` buries the id it is handed, about 170 bytes for the retention window; the record is not capped, since a cap would evict a real logout's record under exactly the flood it bounds, so it holds one entry per logout or rotation of a carried session per window.
- `Manager.CloseWithContext` does **not** close the storage it was handed. A storage registered as a service is closed by the container that created it, so a manager that closed it too would close it twice. Set `OwnsStorage` on the [`session.ManagerConfig`](../../session/manager.go) when you build both by hand and want one `CloseWithContext` to end both — and not for a storage that is also a registered service, which would bring the double close back.
- `ManagerConfig.Ttl` is a [`bound.Duration`](../../bound/limit.go): left out it is 24 hours, a value below one second **panics** at construction naming the field — such a session lapses before the client can ever come back with its cookie, and zero or a negative value is not a way to ask for no expiry — and `bound.UnlimitedDuration()` is the explicit way to ask for sessions that never lapse, which the in-process storage cannot afford on a public path.
- **The session `RegenerateSession` returns must be published with [`http.SetRequestSession`](../../http/request_session.go)** — [`http.RegenerateRequestSession`](../../http/session.go) does it for you, and is what a handler should call. The response path reads the published session to decide what to save. A forgotten republish does not merely skip the rotation: the session that went in is latched **out of use**, so the response path takes the clearing branch, expires the browser cookie and the client is cleanly logged out, receiving a fresh session on its next request. Without that clearing the client would be left presenting an id `RegenerateSession` had already deleted, with no `Set-Cookie` to correct it — a login that loops back to the form on every attempt with nothing ever stored. Being logged out is the safe failure, not the intended outcome.
- Write the authenticated identity to the session `RegenerateSession` **returns**. The one that went in is cleared, and `Clear` latches, so a rotated-away session cannot be resurrected by whatever is written to it afterwards: the response path still deletes the old id and emits the clearing cookie. A **foreign** `Session` implementation is cleared through its own `Clear`, which may or may not latch, so an application supplying its own `Session` should give it the same behaviour or avoid writing to the rotated-away object.
- **A storage that cannot answer is an error, never a panic.** [`Manager.Session`](../../session/manager.go) and [`NewSession`](../../session/manager.go) answer `nil` and the storage's error marked with [`exception.MarkInfrastructureFailure`](../../exception/infrastructure_failure.go); `SaveSession`, `DeleteSession` and `RegenerateSession` mark every error of the storage the same way, a full storage's refusal included, the storage's own error kept in the chain. 128 minted ids in a row already held is an unmarked error. The http kernel loads the session at its first read, [`http.RequestSession`](../../http/request_session.go), which answers a load or mint failure to its reader, filed once: a handler returning a marked one answers `503` with the kernel's `Retry-After`, any other `500`, and no cookie is written; a request that never reads the session makes no storage call. A save that fails in the response path stays the empty `500` with no cookie, since the handler already ran.
- **An outage can serve an empty session, opt-in.** With [`httpcontract.SessionPolicy{EmptyOnOutage}`](../../http/contract/kernel.go) on, `ApplicationConfig.SessionEmptyOnOutage` or `MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE` (see [CONFIG.md](CONFIG.md#session-storage-outage)), `http.RequestSession` answers a load or mint the storage could not answer with a [`session.UnavailableSession`](../../session/unavailable_session.go) and no error, and files one Warning record for the request; an unmarked error stays an error. The session has no id and keeps what the request writes for the request alone: `SaveSession` and `RegenerateSession` refuse it before any storage call, marked, with `session.ErrSessionUnavailable` in the chain, and the response path writes no cookie and expires none, so the client finds its session when the storage answers again. [`session.SessionUnavailableCause`](../../session/unavailable_session.go) tells it apart: a handler that must not lose a write returns the cause, answered `503` with `Retry-After`, and a token source that reads the session answers the unavailable token (see [SECURITY.md](SECURITY.md#sign-in-and-sign-out-doors)). `RegenerateRequestSession` refuses it itself, whatever manager is registered, answering the filed cause, so a handler returning that refusal adds no record. A decorator of a session forwards `SessionUnavailableCause`, since the response path and the manager read the capability on the session they are handed.
- `RegenerateSession` mints the fresh id through the helper `NewSession` mints through, so a storage that fails while probing is the marked error. The returned `error` also covers a nil session, a malformed incoming id, an id a logout or an earlier rotation already buried, and a failure to remove the previous entry — that last path is the one the fail-safe depends on: a failed delete whose entry is still there deliberately leaves the caller a session it can keep using, while one whose entry a fresh read says is gone buries the id, so the copy in hand cannot write the ended session back. The fresh id is minted *before* the previous entry is removed, so such a failure leaves the session still in use intact.
- Rotation carries values over with `Session.All()`, which copies maps and slices all the way down. A value of any other kind — a pointer, or a struct holding one — is still shared between the old and the new session object, and between the concurrent requests that loaded the same session; the old object is discarded, so this is normally invisible, but a pointer put into a session is shared state and mutating it concurrently is a data race the session's own lock cannot cover.
- [`FileStorage`](../../session/file_storage.go) is recommended for development only; production deployments should use a dedicated session backend. Five things follow from how it is built, and all five are why: it reads its snapshot **once**, at construction, and every write rewrites the whole file, so two processes pointed at one path erase each other's sessions; **that whole-file rewrite is also what it costs**, since every `Save`, every `Delete` that removes an entry and every `Load` that finds a lapsed entry re-encodes and `fsync`s the entire map — measured at ~6.7ms per save over 100 sessions, ~9.1ms over 1 000 and ~30ms over 10 000, which is a ceiling of a few dozen writes a second on a store that has been up for a while, and the number of sessions it is paying for is everyone's, not the caller's; a handle given as the `File` of [`NewFileStorage`](../../session/file_storage.go) must not be shared with another writer — each write rewrites the whole snapshot from offset zero and truncates
  to its own length, so two writers interleave whole-file rewrites with no coordination — and must not be opened for appending, which is refused at construction because an appending write ignores the offset the snapshot names, nor read-only, which is refused at construction too, since it would load the sessions and then fail every save (the access mode is read from the descriptor on every Unix system — Linux, macOS, the BSDs, Solaris, illumos and AIX; on Windows, Plan 9 and WebAssembly the first save reports it); values round-trip through JSON, so a session that outlives a restart comes back with every number as a `float64` — an `int64` beyond 2^53 comes back changed; and that same handle door **cannot be atomic across a crash**, because it writes through a handle the caller owns and a rename would unlink the inode that caller still holds. It gives what it can instead: the snapshot is encoded whole before a byte is written, the write names offset zero rather than seeking to it, the write
  precedes the truncation, and the truncation cuts to the length just written, so no kill can leave a zero-length file and log every user out silently. A kill landing inside the write itself can still leave a torn document, and the next construction reports it as a decode failure rather than reading it as an empty session set. Where the atomicity matters, hand the `Path` and let [`NewFileStorage`](../../session/file_storage.go) own the file. [`InMemoryStorage`](../../session/in_memory_storage.go) keeps native types and has none of these, being a single map in one process.
- **Both storages hold a bounded number of sessions.** At `MaxSessions`, once the lapsed sessions are swept, a `Save` of a new id is refused with `the in-memory session storage is full` or `the session file storage is full`, and a `Save` of a stored id still lands, so no live session is logged out. The kernel answers a refused save with a 500 and no session cookie, and only logs it once the response is committed. A file already holding more than the ceiling keeps what it holds. The framework's fallback storage carries the default, so a public route that writes to the session cannot grow the process past it; its ceiling is `MELODY_HTTP_SESSION_MAX_SESSIONS`, 100,000 by default, unlimited admitted, and an application expecting more sessions raises the key or registers its own storage. Beside `MELODY_HTTP_SESSION_TTL=unlimited` the fallback storage writes the boot warning `http.session.unboundedTtlOnBoundedDefaultStorage`, and an unlimited ceiling there is refused at the http boot (see [CONFIG](CONFIG.md#session-max-sessions)).
- Both storages refuse `Load`, `Save` and `Delete` after `CloseWithContext`, and both treat the expiry instant itself as lapsed. **They keep the expiry on different clocks, deliberately.** `InMemoryStorage` keeps the expiry as an in-process `time.Time`, which carries Go's monotonic reading: an NTP step neither lapses nor resurrects a session, and no entry survives a restart anyway. `FileStorage` persists `expiresAt` as unix nanoseconds — the wall reading is the only one that can survive a restart — so its expiry follows every step of the wall clock. An application that moves between the two keeps the same boundary (the instant itself is lapsed on both) but trades monotonic stability for restart durability. Every instant comes from the injected clock: the `Clock` of `InMemoryStorageConfig` and `FileStorageConfig`, nil the system clock, and the framework-wired default storage runs on the kernel's clock — so a frozen clock in a test moves session time deterministically. Neither serialises anything across
  processes: `Storage.Save` is a blind upsert, so two concurrent requests writing different keys of the same session end with whichever wrote last, and a deployment running more than one node needs a storage of its own that makes those writes conditional.

## Userland API

### Contracts (`session/contract`)

#### Types

- [`type Manager`](../../session/contract/manager.go)
    - `Session(sessionId string) (Session, error)`
    - `NewSession() (Session, error)`
    - `SaveSession(session Session) error`
    - `DeleteSession(sessionId string) error`
    - `RegenerateSession(session Session) (Session, error)`
    - `CloseWithContext(closeContext context.Context) error`
- [`type Storage`](../../session/contract/storage.go)
- [`type Session`](../../session/contract/session.go)
    - `Snapshot() (values map[string]any, modified bool, cleared bool)`

### Types

- [`type Manager`](../../session/manager.go)
    - [`RegenerateSession(session sessioncontract.Session) (sessioncontract.Session, error)`](../../session/manager.go) — rotates the id, carrying the values over; see [Rotating the session id](#rotating-the-session-id)
- [`type Session`](../../session/session.go)
    - [`Snapshot() (map[string]any, bool, bool)`](../../session/session.go) — the values, the modified flag and the cleared flag read under one lock acquisition
- [`func Snapshot(sessionInstance sessioncontract.Session) (map[string]any, bool, bool)`](../../session/snapshot.go) — a session's values and flags, through the session's own `Snapshot`
- [`type InMemoryStorage`](../../session/in_memory_storage.go)
    - [`Clear() error`](../../session/in_memory_storage.go) — drops every stored session, refusing once the storage is closed
- [`type FileStorage`](../../session/file_storage.go) — the storage the [durability](#file-storage-durability) section is about, recommended for development only
- **session.ManagerConfig** ([`session/manager.go`](../../session/manager.go)) — `Ttl`, `TombstoneRetention`, `Clock` and `OwnsStorage`, the zero value the default
- **session.InMemoryStorageConfig** ([`session/in_memory_storage.go`](../../session/in_memory_storage.go)) — `CleanupInterval`, 1m by default, at least 1ms, unlimited refused, `MaxSessions`, 100,000 by default, at least 1, unlimited admitted, and `Clock`
- **session.FileStorageConfig** ([`session/file_storage.go`](../../session/file_storage.go)) — `Path` or `File`, `MaxSessions`, 100,000 by default, at least 1, unlimited admitted, and `Clock`

### Constants

- [`DefaultMaxSessions`](../../session/in_memory_storage.go) — 100,000, the sessions a storage holds when its `MaxSessions` is left out
- [`SessionCookieName`](../../session/const.go) — `MELODYSESSID`, the name of the browser cookie every passage below calls "the session cookie"

### Constructors

- `session.NewManager(storage, config) *session.Manager` ([`session/manager.go`](../../session/manager.go)) — the zero config is a 24-hour ttl, a five-minute tombstone window and the system clock, and does not own the storage, so `CloseWithContext` leaves it open; `OwnsStorage` closes it with the manager
- `session.NewInMemoryStorage(config) *session.InMemoryStorage` ([`session/in_memory_storage.go`](../../session/in_memory_storage.go)) — the zero config sweeps every minute on the system clock; `Clock` reads every expiry instant and the sweep's tick from the injected clock
- `session.NewUnavailableSession(cause) *session.UnavailableSession` ([`session/unavailable_session.go`](../../session/unavailable_session.go)) — the empty session of a request whose storage could not answer, carrying `sessioncontract.SessionUnavailable`; a nil cause is refused naming the argument, and [`SessionUnavailableCause`](../../session/unavailable_session.go) answers the cause of one, false for any other session
- `session.NewFileStorage(config) (*session.FileStorage, error)` ([`session/file_storage.go`](../../session/file_storage.go)) — exactly one of `Path`, a file the storage owns and writes atomically, and `File`, a handle the caller owns, both or neither refused; `Clock` reads every expiry instant from the injected clock

### Container helpers

- [`const ServiceSessionManager`](../../session/service_resolver.go)
- [`const ServiceSessionStorage`](../../session/service_resolver.go)
- [`SessionMustFromContainer(context.Context, containercontract.Container)`](../../session/service_resolver.go)
- [`SessionStorageMustFromContainer(context.Context, containercontract.Container)`](../../session/service_resolver.go)
- [`SessionStorageMustFromResolver(context.Context, containercontract.Resolver)`](../../session/service_resolver.go)
