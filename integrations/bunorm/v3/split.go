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

/* Reader selects replicas round-robin and falls back to the primary only for an operational open failure. Empty or unknown names, a closed registry and a nil-database provider are returned as errors. If both opens fail, the primary error is the cause and the replica failure remains in the diagnostic context. */
func (instance *ReadWriteSplitter) Reader() (*bun.DB, error) {
    readerName := instance.ReaderName()

    database, databaseErr := instance.registry.Database(readerName)
    if nil == databaseErr {
        return database, nil
    }

    if true == errors.Is(databaseErr, ErrProviderDefinitionNotFound) ||
        true == errors.Is(databaseErr, ErrProviderDefinitionNameIsRequired) ||
        true == errors.Is(databaseErr, ErrManagerRegistryClosed) ||
        true == errors.Is(databaseErr, ErrProviderReturnedNilDatabase) {
        return nil, databaseErr
    }

    var invalidParameters interface { ConnectionParametersInvalid() bool }
    if true == errors.As(databaseErr, &invalidParameters) && true == invalidParameters.ConnectionParametersInvalid() {
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
