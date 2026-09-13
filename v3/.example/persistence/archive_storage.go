package persistence

import (
    "context"
    bun "github.com/uptrace/bun"
)

/* ServiceArchiveStorage is the container name of the handle onto the archive database. */
const ServiceArchiveStorage = "service.example.archive.storage"

/* ServiceArchiveLocker is the container name of the locker the archive's writers take, and it lives here rather than in the configuration package because both ends need it: the configuration registers it, and the command that writes the archive resolves it — and a command may not import the configuration, which imports the commands.

   It is a name of its own rather than the framework's ServiceLocker, and the distinction is the point: this application already publishes a locker under that name — redis when configured, otherwise mysql, otherwise in memory — and that is the general-purpose one every handler and the cron tick reach for. This one is bound to the archive database, so a process writing the archive is excluded by the very server it is writing to, without depending on redis being up. The pgsql locker's own contract is what makes it right for that: an advisory lock is held for exactly as long as the backend session that took it, so a process that dies mid-refresh releases it when its connection drops, with no lease to expire and nothing to clean up. */
const ServiceArchiveLocker = "service.example.archive.locker"

/* ArchiveStorage is where the catalogue's readings are kept, or the absence of anywhere to keep them.

   It exists for the same reason CatalogStorage does, and it is worth writing down because the shape looks like ceremony until the generator is taken into account: melody:wiring:generate fills a constructor's arguments by resolving them from the container BY TYPE, so a repository constructor asking for a *bun.DB directly could only be generated for an application that has one — and this example is meant to boot with either database, both, or neither. Asking for this handle instead moves the question out of the generated wiring and into a package the generator does not scan, where it can be answered honestly.

   The second reason is this major's own: two *bun.DB handles are registered here, and neither is on the container's type index, so a resolution by type could not tell them apart even if the generator tried. */
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
