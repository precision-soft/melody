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

/* Reader answers the round-robin replica, and falls back to the primary only when the replica is unreachable, an open failure filed under ErrDatabaseUnreachable. Every other failure, a wiring error or a teardown in progress, is refused, so a dead replica configuration is never served silently from the primary. When the primary fails too, the answer carries its failure as the cause and names the replica failure beside it. */
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
