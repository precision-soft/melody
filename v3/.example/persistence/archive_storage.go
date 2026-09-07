package persistence

import (
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
}

func NewArchiveStorage(database *bun.DB) *ArchiveStorage {
    return &ArchiveStorage{database: database}
}

/* Database is nil when the environment configured no archive connection. */
func (instance *ArchiveStorage) Database() *bun.DB {
    return instance.database
}

func (instance *ArchiveStorage) IsPersistent() bool {
    return nil != instance.database
}
