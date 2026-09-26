package persistence

import (
    "context"
    bun "github.com/uptrace/bun"
)

/* ServiceArchiveStorage is the container name of the handle onto the archive database. */
const ServiceArchiveStorage = "service.example.archive.storage"

/* ServiceArchiveLocker is the container name of the locker the archive's writers take, declared here because both the configuration that registers it and the command that resolves it need it, and a command may not import the configuration. It is a name of its own rather than the framework's general-purpose ServiceLocker because it is bound to the archive database: a pgsql advisory lock lives exactly as long as the session that took it, so a process that dies mid-refresh releases it when its connection drops. */
const ServiceArchiveLocker = "service.example.archive.locker"

/* ArchiveStorage is where the catalogue's readings are kept, or the absence of anywhere to keep them. melody:wiring:generate fills a constructor's arguments by type, so a repository asking for a *bun.DB could only be generated for an application that has one; this handle moves the question into a package the generator does not scan, and both *bun.DB handles are kept off the type index. */
type ArchiveStorage struct {
    database *bun.DB
    location string
    context  context.Context
}

func NewArchiveStorage(database *bun.DB) *ArchiveStorage {
    return NewArchiveStorageAt(database, "")
}

/* NewArchiveStorageAt names the database the handle is open on, the way the catalogue handle is named: the reset prints both before it destroys either, and two databases told apart only by their tables is what that plan exists to prevent. */
func NewArchiveStorageAt(database *bun.DB, location string) *ArchiveStorage {
    return &ArchiveStorage{database: database, location: location}
}

/* WithContext binds the process's signal context to the handle, for the work its first resolution does beyond the dial — applying the archive's migration set, which waits up to the lock window on a held migration lock — so a SIGTERM during that first resolution ends it the way it ends the dial; without one the work runs under a background context, which is the honest state of a handle a test built. */
func (instance *ArchiveStorage) WithContext(ctx context.Context) *ArchiveStorage {
    instance.context = ctx

    return instance
}

/* Context is the context the handle's first-resolution work runs under: the process's signal context when the composition root bound one, a background context otherwise. */
func (instance *ArchiveStorage) Context() context.Context {
    if nil == instance.context {
        return context.Background()
    }

    return instance.context
}

/* Database is nil when the environment configured no archive connection. */
func (instance *ArchiveStorage) Database() *bun.DB {
    return instance.database
}

func (instance *ArchiveStorage) IsPersistent() bool {
    return nil != instance.database
}

/* Location names the database the handle is open on, as the connection was declared, and is empty for a handle nobody located. */
func (instance *ArchiveStorage) Location() string {
    return instance.location
}
