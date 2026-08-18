# SESSION

The [`session`](../../session) package provides a small session subsystem for Melody: session ids, in-memory storage, and a manager that persists modified sessions.

## Scope

- Package: [`session/`](../../session)
- Subpackage: [`session/contract/`](../../session/contract)

## Subpackages

- [`session/contract`](../../session/contract)  
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
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/session"
)

func updateSession(
	serviceContainer containercontract.Container,
	sessionId string,
) error {
	manager := session.SessionMustFromContainer(serviceContainer)

	sessionInstance := manager.Session(sessionId)
	if nil == sessionInstance {
		sessionInstance = manager.NewSession()
	}

	sessionInstance.Set("userId", "u-123")

	return manager.SaveSession(sessionInstance)
}
```

## Session id validation

Session ids are always 32-character lowercase hex strings. Incoming cookies that do not match this shape are rejected by [`Manager.Session`](../../session/manager.go) without touching storage, and [`DeleteSession`](../../session/manager.go) refuses malformed ids. New ids are generated from 16 bytes of `crypto/rand`.

## File storage durability

[`NewFileStorageFromPath`](../../session/file_storage.go) flushes modifications atomically via `os.CreateTemp` + `os.Rename`, and fsyncs the parent directory after the rename, so a crash mid-write cannot leave a truncated session file on disk and a power loss right after a save cannot resurface the previous snapshot. A hard kill between the temp file and the rename leaves an orphan `<name>.<random>.tmp` beside the store — a complete snapshot of every live session — and the next construction sweeps its own temp spelling away before reading. File mode `0755` is used for the parent directory.

## Rotating the session id

[`Manager.RegenerateSession`](../../session/manager.go) is the defence against **session fixation**: it mints a fresh id, carries the current values over to it, and removes the storage entry the previous id pointed at. Rotate on any privilege change — above all at login, so an id an attacker managed to plant in the victim's browser before authentication is not the id that ends up carrying the authenticated identity.

Rotation lives on the `Manager` rather than on the `Session` because only the manager holds the storage the candidate id is probed against and the previous entry is deleted from; a `Session` keeps no storage reference.

Place the rotation **before** the handler writes the authenticated identity, and reach for [`http.RegenerateRequestSession`](../../http/session.go) rather than the manager: it rotates and republishes in one call, so the two halves cannot be half-done.

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
- `Session.All()` returns a copy that reaches all the way down, the depth both storages copy at. Mutating the returned map, or anything nested inside it, does not touch the live session — which also means such a mutation is not persisted, because it never passed through `Set`.
- A session another request deleted cannot be saved again — within one process. `Manager.DeleteSession` remembers the id for [`session.TombstoneRetention`](../../session/manager.go) by default — the window is configurable through `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` or [`session.NewManagerWithTombstoneRetention`](../../session/manager.go) — and `SaveSession` refuses a write under it, returning an error whose cause is [`session.ErrSessionDeleted`](../../session/manager.go); a rotated-away id is remembered the same way. Without it a request that loaded the session before a logout deleted it would re-create the entry when its own handler finished. The response path answers that refusal by expiring the browser cookie and serving the handler's response unchanged — it is the session ending, not a storage failure. The record lives in the manager's own memory, not in the storage it guards: a shared storage does not carry it between instances, so on a deployment with more than one node a
  logout served by one instance does not stop a peer instance's in-flight request from writing the entry back when its handler finishes. The record is bounded by the window, not by how many sessions were ever deleted.
- `Manager.Close` does **not** close the storage it was handed. A storage registered as a service is closed by the container that created it, so a manager that closed it too would close it twice. Use [`session.NewManagerOwningStorage`](../../session/manager.go) when you build both by hand and want one `Close` to end both — and not for a storage that is also a registered service, which would bring the double close back.
- A negative `ttl` **panics** at construction. Both storages treat any `ttl` that is not positive as "no expiry", so a negative one would produce the immortal session rather than the already-lapsed one it reads like. A positive `ttl` below one second is refused the same way — such a session lapses before the client can ever come back with its cookie. Zero remains the explicit way to ask for no expiry.
- **The session `RegenerateSession` returns must be republished on `RequestAttributeSession`** — [`http.RegenerateRequestSession`](../../http/session.go) does it for you, and is what a handler should call. The response path re-reads that attribute to decide what to save. A forgotten republish does not merely skip the rotation: the session that went in is latched **out of use**, so the response path takes the clearing branch, expires the browser cookie and the client is cleanly logged out, receiving a fresh session on its next request. Without that clearing the client would be left presenting an id `RegenerateSession` had already deleted, with no `Set-Cookie` to correct it — a login that loops back to the form on every attempt with nothing ever stored. Being logged out is the safe failure, not the intended outcome.
- Write the authenticated identity to the session `RegenerateSession` **returns**. The one that went in is cleared, and `Clear` latches, so a rotated-away session cannot be resurrected by whatever is written to it afterwards: the response path still deletes the old id and emits the clearing cookie. A **foreign** `Session` implementation is cleared through its own `Clear`, which may or may not latch, so an application supplying its own `Session` should give it the same behaviour or avoid writing to the rotated-away object.
- `RegenerateSession` **panics** rather than returning an error when storage fails while probing for a unique id, matching [`NewSession`](../../session/manager.go) — both mint through the same helper. The returned `error` covers a nil session, a malformed incoming id, and a failure to remove the previous entry — that third path is the one the fail-safe depends on, since a failed delete deliberately leaves the caller a session it can keep using. The fresh id is minted *before* the previous entry is removed, so such a failure leaves the session still in use intact.
- Rotation carries values over with `Session.All()`, which copies maps and slices all the way down. A value of any other kind — a pointer, or a struct holding one — is still shared between the old and the new session object, and between the concurrent requests that loaded the same session; the old object is discarded, so this is normally invisible, but a pointer put into a session is shared state and mutating it concurrently is a data race the session's own lock cannot cover.
- [`FileStorage`](../../session/file_storage.go) is recommended for development only; production deployments should use a dedicated session backend. Five things follow from how it is built, and all five are why: it reads its snapshot **once**, at construction, and every write rewrites the whole file, so two processes pointed at one path erase each other's sessions; **that whole-file rewrite is also what it costs**, since every `Save`, every `Delete` that removes an entry and every `Load` that finds a lapsed entry re-encodes and `fsync`s the entire map — measured at ~6.7ms per save over 100 sessions, ~9.1ms over 1 000 and ~30ms over 10 000, which is a ceiling of a few dozen writes a second on a store that has been up for a while, and the number of sessions it is paying for is everyone's, not the caller's; a handle given to [`NewFileStorageFromFile`](../../session/file_storage.go) must not be shared with another writer — each write rewrites the whole snapshot from offset zero and
  truncates to its own length, so two writers interleave whole-file rewrites with no coordination — and must not be opened for appending, which is refused at construction because an appending write ignores the offset the snapshot names; values round-trip through JSON, so a session that outlives a restart comes back with every number as a `float64` — an `int64` beyond 2^53 comes back changed; and that same handle door **cannot be atomic across a crash**, because it writes through a handle the caller owns and a rename would unlink the inode that caller still holds. It gives what it can instead: the snapshot is encoded whole before a byte is written, the write names offset zero rather than seeking to it, the write precedes the truncation, and the truncation cuts to the length just written, so no kill can leave a zero-length file and log every user out silently. A kill landing inside the write itself can still leave a torn document, and the next construction reports it as a decode failure
  rather than reading it as an empty session set. Where the atomicity matters, hand the path and let [`NewFileStorageFromPath`](../../session/file_storage.go) own the file. [`InMemoryStorage`](../../session/in_memory_storage.go) keeps native types and has none of these, being a single map in one process.
- Both storages refuse `Load`, `Save` and `Delete` after `Close`, and both treat the expiry instant itself as lapsed. Neither serialises anything across processes: `Storage.Save` is a blind upsert, so two concurrent requests writing different keys of the same session end with whichever wrote last, and a deployment running more than one node needs a storage of its own that makes those writes conditional.

## Userland API

### Contracts (`session/contract`)

#### Types

- [`type Manager`](../../session/contract/manager.go)
    - `Session(sessionId string) Session`
    - `NewSession() Session`
    - `RegenerateSession(session Session) (Session, error)`
    - `SaveSession(session Session) error`
    - `DeleteSession(sessionId string) error`
    - `Close() error`
- [`type Storage`](../../session/contract/storage.go)
- [`type Session`](../../session/contract/session.go)

### Types

- [`type Manager`](../../session/manager.go)
    - [`RegenerateSession(session sessioncontract.Session) (sessioncontract.Session, error)`](../../session/manager.go) — rotates the id, carrying the values over; see [Rotating the session id](#rotating-the-session-id)
- [`type Session`](../../session/session.go)
- [`type InMemoryStorage`](../../session/in_memory_storage.go)
    - [`Clear() error`](../../session/in_memory_storage.go) — drops every stored session, refusing once the storage is closed
- [`type FileStorage`](../../session/file_storage.go) — the storage the [durability](#file-storage-durability) section is about, recommended for development only

### Constants

- [`SessionCookieName`](../../session/const.go) — `MELODYSESSID`, the name of the browser cookie every passage below calls "the session cookie"

### Constructors

- [`session.NewManager(storage, ttl)`](../../session/manager.go) — does not own the storage; `Close` leaves it open
- [`session.NewManagerWithTombstoneRetention(storage, ttl, tombstoneRetention)`](../../session/manager.go) — sizes the write-back refusal window to the deployment; refuses a zero or negative window with a panic
- [`session.NewManagerOwningStorage(storage, ttl)`](../../session/manager.go) — closes the storage when the manager is closed
- [`session.NewInMemoryStorage()`](../../session/in_memory_storage.go)
- [`session.NewInMemoryStorageWithCleanupInterval(cleanupInterval)`](../../session/in_memory_storage.go)
- [`session.NewFileStorageFromPath(path)`](../../session/file_storage.go)
- [`session.NewFileStorageFromFile(file)`](../../session/file_storage.go)

### Container helpers

- [`const ServiceSessionManager`](../../session/service_resolver.go)
- [`const ServiceSessionStorage`](../../session/service_resolver.go)
- [`SessionMustFromContainer(containercontract.Container)`](../../session/service_resolver.go)
- [`SessionStorageMustFromContainer(containercontract.Container)`](../../session/service_resolver.go)
- [`SessionStorageMustFromResolver(containercontract.Resolver)`](../../session/service_resolver.go)
