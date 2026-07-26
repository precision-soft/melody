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
- Store a value either isolated from every other request (`Set`) or as the one handle every reader reaches (`SetShared`).
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
	containercontract "github.com/precision-soft/melody/v3/container/contract"
	"github.com/precision-soft/melody/v3/session"
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

## Copied and shared values

The **write** decides what a session key holds; the read honours whichever was chosen.

[`Session.Set`](../../session/session.go) stores a value the storage layer is free to copy, and copy it does: every entry is deep-copied on the way into the store and on the way out of it ([`internal.CopyAnyMap`](../../internal/copy.go), which follows pointers, slices, maps and exported struct fields). That is what keeps a session read on one request from writing into another request's data, and it is the default because a copy is the semantics that cannot be got wrong by accident — nobody writes through a copy and expects the write to land somewhere else.

[`Session.SetShared`](../../session/session.go) stores the value **itself**. `Get` hands back that very handle, and every reader of the session reaches the one object. It is for what a copy would break: a value whose identity is the point rather than its contents — a live connection, a subscription, a counter several requests have to agree on.

```go
/* data: isolated per request, serialisable, safe on any storage */
sessionInstance.Set("locale", "ro")

/* a handle: identity is the point, so the very value is what comes back */
sessionInstance.SetShared("uploadProgress", progressTracker)
```

There is deliberately **no `GetShared` and no `IsShared`**. `Get` already answers whichever way the value was written, so a reading counterpart could only restate an intent it has no power to change. `All` answers readers the same way — a shared value comes out as the handle, never as the envelope the storage path files it under.

A value that came back out of the store is therefore read, modified and written back. What `Get` returns is this request's own copy, so mutating it in place changes nothing the store holds — and, no `Set` having followed, it leaves `IsModified()` false, so [`Manager.SaveSession`](../../session/manager.go) returns early and writes nothing at all:

```go
/* lost: the write lands in a copy, and the session is never marked modified */
sessionInstance.Get("cart").(*Cart).Items = append(sessionInstance.Get("cart").(*Cart).Items, item)

/* kept: read, modify, write back */
cart := sessionInstance.Get("cart").(*Cart)
cart.Items = append(cart.Items, item)
sessionInstance.Set("cart", cart)
```

A rule of thumb: anything that is *data* — a user id, a csrf token, a locale, a flash message — takes `Set` and stays portable to any storage. `SetShared` buys identity at the price of portability: the value lives in this process, so it confines the session to a storage that keeps values in the process.

## File storage durability

[`NewFileStorageFromPath`](../../session/file_storage.go) flushes modifications atomically via `os.CreateTemp` + `os.Rename` so a crash mid-write cannot leave a truncated session file on disk. File mode `0755` is used for the parent directory.

### A shared value is refused rather than persisted

[`FileStorage.Save`](../../session/file_storage.go) keeps sessions as json on disk, and nothing it can write loads back as the same handle. It therefore **refuses to save a session holding a value stored with `SetShared`**, with `session value stored with set shared cannot be persisted by the file storage` and the offending **key** in the error context — the session id is not named, because it is a credential and errors get logged.

Refusing is the only honest answer available: encoding the envelope would put an empty object in the file and hand the next request a session that looks intact and has quietly lost what was shared through it, which is precisely the failure the copied/shared distinction exists to make visible. The refusal is whole-session and happens before anything is touched, matching how the rest of this storage commits — a `Save` that returns an error leaves no trace of itself.

On the http response path the refusal degrades the same way any other storage failure does ([`router_utility.go`](../../http/router_utility.go)): it is logged as `failed to save session` and the response is sent **without** a session cookie, so no browser is pointed at a session that was never written.

[`InMemoryStorage`](../../session/in_memory_storage.go) keeps its values in the process, so a shared value survives there — the envelope carries no exported field for the deep copy to duplicate, and the handle inside arrives as itself.

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

- **`Get` hands back a copy of what was stored with `Set`, so mutating in place loses the write.** `sessionInstance.Get("cart").(*Cart).Items = append(...)` writes into the copy this request was handed when it loaded the session, and does two nothings at once: the stored entry is a different object, and an in-place write never touches `modified`, so `Manager.SaveSession` takes its early return and writes nothing at all. The remedy is the ordinary value discipline — read, modify, `Set` back — or `SetShared` when the identity of the object is the point rather than its contents. Worked through in [Copied and shared values](#copied-and-shared-values). This is the single most likely way to get sessions wrong.
- **A session holding a shared value cannot be saved by [`FileStorage`](../../session/file_storage.go)**, which refuses the whole session and names the key; see [A shared value is refused rather than persisted](#a-shared-value-is-refused-rather-than-persisted). A value stored with `SetShared` therefore ties the application to a storage that keeps values in the process.
- `Manager.SaveSession` only persists when `Session.IsModified()` is true; a read-only session is not written.
- Clearing a session (`Session.Clear()`) marks it as cleared; saving a cleared session deletes it.
- `Session.All()` returns a copy of the internal map.
- **The session `RegenerateSession` returns must be republished on `RequestAttributeSession`** — [`http.RegenerateRequestSession`](../../http/session.go) does it for you, and is what a handler should call. The response path re-reads that attribute to decide what to save. A forgotten republish does not merely skip the rotation: the session that went in is latched **out of use**, so the response path takes the clearing branch, expires the browser cookie and the client is cleanly logged out, receiving a fresh session on its next request. Without that clearing the client would be left presenting an id `RegenerateSession` had already deleted, with no `Set-Cookie` to correct it — a login that loops back to the form on every attempt with nothing ever stored. Being logged out is the safe failure, not the intended outcome.
- Write the authenticated identity to the session `RegenerateSession` **returns**. The one that went in is latched out of use rather than merely cleared, so a rotated-away session cannot be resurrected by whatever is written to it afterwards: `Session.Set` lifts the cleared flag, and nothing lifts the latch, so the response path still deletes the old id and emits the clearing cookie. The one caveat is a **foreign** `Session` implementation, which the manager can only `Clear()` — a later write does undo that — so an application supplying its own `Session` must still avoid writing to the abandoned object.
- `RegenerateSession` **panics** rather than returning an error when storage fails while probing for a unique id, matching [`NewSession`](../../session/manager.go) — both mint through the same helper. The returned `error` covers a nil session, a malformed incoming id, and a failure to remove the previous entry — that third path is the one the fail-safe depends on, since a failed delete deliberately leaves the caller a session it can keep using. The fresh id is minted *before* the previous entry is removed, so such a failure leaves the session still in use intact.
- Rotation carries the values over as they are stored, envelopes intact, so a session that survives a regenerated id keeps the handles it was holding. The carry-over is a shallow copy of the map: a mutable value (a slice, a map, a pointer) is reachable from both the old and the new session object. Since the old one is latched out of use this is normally invisible, but do not keep using it.
- [`FileStorage`](../../session/file_storage.go) is recommended for development only; production deployments should use a dedicated session backend.

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
    - `Get(key string) any` — returns the handle for a value written with `SetShared`, a copy of what the storage kept for one written with `Set`
    - `Set(key string, value any)` — stores a value the storage layer may copy
    - `SetShared(key string, value any)` — stores the value itself; see [Copied and shared values](#copied-and-shared-values)

### Types

- [`type Manager`](../../session/manager.go)
    - [`RegenerateSession(session sessioncontract.Session) (sessioncontract.Session, error)`](../../session/manager.go) — rotates the id, carrying the values over; see [Rotating the session id](#rotating-the-session-id)
- [`type Session`](../../session/session.go)

### Constructors

- [`session.NewManager(storage, ttl)`](../../session/manager.go)
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
