# SESSION

The [`session`](../../session) package provides a small session subsystem for Melody: session ids, in-memory storage, and a manager that persists modified sessions.

## Scope

- Package: `session/`
- Subpackage: `session/contract/`

## Subpackages

- [`session/contract`](../../session/contract)  
  Public contracts (`Manager`, `Storage`, `Session`).

## Responsibilities

- Create and load sessions through a `Manager` (`NewSession`, `Session`).
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
package example

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

## Footguns & caveats

- `Manager.SaveSession` only persists when `Session.IsModified()` is true; a read-only session is not written.
- Clearing a session (`Session.Clear()`) marks it as cleared; saving a cleared session deletes it.
- `Session.All()` returns a copy of the internal map.

## Userland API

### Contracts (`session/contract`)

#### Types

- [`type Manager`](../../session/contract/manager.go)
- [`type Storage`](../../session/contract/storage.go)
- [`type Session`](../../session/contract/session.go)

### Types

- [`type Manager`](../../session/manager.go)
- [`type Session`](../../session/session.go)

### Constructors

- [`session.NewManager(storage, ttl)`](../../session/manager.go)
- [`session.NewInMemoryStorage()`](../../session/in_memory_storage.go)
- [`session.NewInMemoryStorageWithCleanupInterval(cleanupInterval)`](../../session/in_memory_storage.go)

### Container helpers

- [`const ServiceSessionManager`](../../session/service_resolver.go)
- [`const ServiceSessionStorage`](../../session/service_resolver.go)
- [`SessionMustFromContainer(containercontract.Container)`](../../session/service_resolver.go)
- [`SessionStorageMustFromContainer(containercontract.Container)`](../../session/service_resolver.go)
- [`SessionStorageMustFromResolver(containercontract.Resolver)`](../../session/service_resolver.go)
