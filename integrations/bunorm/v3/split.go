package bunorm

import (
    "errors"
    "sync/atomic"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
)

func NewReadWriteSplitter(registry *ManagerRegistry, primaryName string, replicaNames ...string) *ReadWriteSplitter {
    if nil == registry {
        exception.Panic(exception.NewError("read/write splitter registry is nil", nil, nil))
    }

    if "" == primaryName {
        exception.Panic(exception.NewError("read/write splitter primary name is empty", nil, nil))
    }

    /* an empty replica name would fail every resolution it is picked for, and the fallback below would route that share of the reads to the primary forever with no signal; it is a wiring error and is refused where it is written */
    for index, replicaName := range replicaNames {
        if "" == replicaName {
            exception.Panic(exception.NewError("read/write splitter replica name is empty", map[string]any{"index": index}, nil))
        }
    }

    return &ReadWriteSplitter{
        registry:     registry,
        primaryName:  primaryName,
        replicaNames: append([]string{}, replicaNames...),
    }
}

type ReadWriteSplitter struct {
    registry     *ManagerRegistry
    primaryName  string
    replicaNames []string
    counter      atomic.Uint64
}

func (instance *ReadWriteSplitter) WriterName() string {
    return instance.primaryName
}

func (instance *ReadWriteSplitter) ReaderName() string {
    if 0 == len(instance.replicaNames) {
        return instance.primaryName
    }

    index := instance.counter.Add(1)
    return instance.replicaNames[(index-1)%uint64(len(instance.replicaNames))]
}

func (instance *ReadWriteSplitter) Writer() (*bun.DB, error) {
    return instance.registry.Database(instance.WriterName())
}

/* Reader answers the round-robin replica, and falls back to the primary only when the replica was UNREACHABLE — an open failure its provider classified as transient and could not get past, the outage where reading from the primary is the availability trade this splitter exists to make, answered by ErrDatabaseUnreachable. Every other failure is refused instead of absorbed: a replica name the registry does not know, an empty name, a closed registry, a provider that answered neither a database nor an error, and every refusal the provider or the server gave by name — a parameter left empty, a password the server refused, a database that does not exist. Each is a wiring error or a teardown in progress, permanent by nature, and folding it into the fallback routed every read to the primary forever with no signal that the replica configuration was dead. The list used to be a denylist of registry sentinels, and every refusal it did not name — the provider's own refusals first among them — fell to the primary by default, silently; keyed on the one class that IS the outage, the default direction is the refusal. When the primary fails too, the answer carries the primary failure as its cause and names the replica failure beside it, so the diagnosis does not point at the wrong database. */
func (instance *ReadWriteSplitter) Reader() (*bun.DB, error) {
    readerName := instance.ReaderName()

    database, databaseErr := instance.registry.Database(readerName)
    if nil == databaseErr {
        return database, nil
    }

    if false == errors.Is(databaseErr, ErrDatabaseUnreachable) {
        return nil, databaseErr
    }

    if readerName == instance.primaryName {
        return nil, databaseErr
    }

    primary, primaryErr := instance.registry.Database(instance.primaryName)
    if nil != primaryErr {
        return nil, exception.NewError(
            "read/write splitter could not open the replica nor the primary",
            map[string]any{
                "replica":      readerName,
                "primary":      instance.primaryName,
                "replicaError": databaseErr.Error(),
            },
            primaryErr,
        )
    }

    return primary, nil
}
