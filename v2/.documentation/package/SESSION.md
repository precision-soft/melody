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
	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/session"
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

[`NewFileStorageFromPath`](../../session/file_storage.go) flushes modifications atomically via `os.CreateTemp` + `os.Rename` so a crash mid-write cannot leave a truncated session file on disk. File mode `0755` is used for the parent directory.

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
- Clearing a session (`Session.Clear()`) marks it as cleared; saving a cleared session deletes it.
- `Session.All()` returns a copy of the internal map.
- **The session `RegenerateSession` returns must be republished on `RequestAttributeSession`** — [`http.RegenerateRequestSession`](../../http/session.go) does it for you, and is what a handler should call. The response path re-reads that attribute to decide what to save. A forgotten republish does not merely skip the rotation: the session that went in is latched **out of use**, so the response path takes the clearing branch, expires the browser cookie and the client is cleanly logged out, receiving a fresh session on its next request. Without that clearing the client would be left presenting an id `RegenerateSession` had already deleted, with no `Set-Cookie` to correct it — a login that loops back to the form on every attempt with nothing ever stored. Being logged out is the safe failure, not the intended outcome.
- Write the authenticated identity to the session `RegenerateSession` **returns**. The one that went in is latched out of use rather than merely cleared, so a rotated-away session cannot be resurrected by whatever is written to it afterwards: `Session.Set` lifts the cleared flag, and nothing lifts the latch, so the response path still deletes the old id and emits the clearing cookie. The one caveat is a **foreign** `Session` implementation, which the manager can only `Clear()` — a later write does undo that — so an application supplying its own `Session` must still avoid writing to the abandoned object.
- `RegenerateSession` **panics** rather than returning an error when storage fails while probing for a unique id, matching [`NewSession`](../../session/manager.go) — both mint through the same helper. The returned `error` covers a nil session, a malformed incoming id, and a failure to remove the previous entry — that third path is the one the fail-safe depends on, since a failed delete deliberately leaves the caller a session it can keep using. The fresh id is minted *before* the previous entry is removed, so such a failure leaves the session still in use intact.
- Rotation carries values over with `Session.All()`, i.e. a shallow copy: a mutable value (a slice, a map, a pointer) is shared between the old and the new session object. Since the old one is discarded this is normally invisible, but do not keep using it.
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
