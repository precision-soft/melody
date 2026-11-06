package bunorm

import (
    "sync"
    "sync/atomic"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
)

/* NewReadWriteSplitter routes writes to the primary and reads round-robin over the replicas. */
func NewReadWriteSplitter(registry *ManagerRegistry, primaryName string, replicaNames ...string) *ReadWriteSplitter {
    return NewReadWriteSplitterWithOptions(registry, primaryName, replicaNames)
}

func NewReadWriteSplitterWithOptions(registry *ManagerRegistry, primaryName string, replicaNames []string, options ...ReadWriteSplitterOption) *ReadWriteSplitter {
    if nil == registry {
        exception.Panic(exception.NewError("read/write splitter registry is nil", nil, nil))
    }

    if "" == primaryName {
        exception.Panic(exception.NewError("read/write splitter primary name is empty", nil, nil))
    }

    /* an empty replica name is a wiring error: the fallback would route its reads to the primary forever, with no signal */
    for index, replicaName := range replicaNames {
        if "" == replicaName {
            exception.Panic(exception.NewError("read/write splitter replica name is empty", map[string]any{"index": index}, nil))
        }
    }

    splitter := &ReadWriteSplitter{
        registry:             registry,
        primaryName:          primaryName,
        replicaNames:         append([]string{}, replicaNames...),
        replicaRetryInterval: defaultReplicaRetryInterval,
        now:                  time.Now,
        unreachableReplicas:  map[string]unreachableReplica{},
    }

    for _, option := range options {
        if nil != option {
            option(splitter)
        }
    }

    return splitter
}

type ReadWriteSplitterOption func(splitter *ReadWriteSplitter)

const defaultReplicaRetryInterval = 5 * time.Second

/* WithReplicaRetryInterval: a zero interval keeps the default of five seconds; a negative one is a wiring error. */
func WithReplicaRetryInterval(interval time.Duration) ReadWriteSplitterOption {
    if 0 > interval {
        exception.Panic(exception.NewError("read/write splitter replica retry interval is negative", map[string]any{"interval": interval.String()}, nil))
    }

    return func(splitter *ReadWriteSplitter) {
        if 0 < interval {
            splitter.replicaRetryInterval = interval
        }
    }
}

type ReadWriteSplitter struct {
    registry     *ManagerRegistry
    primaryName  string
    replicaNames []string
    counter      atomic.Uint64

    replicaRetryInterval time.Duration
    now                  func() time.Time

    unreachableMutex    sync.Mutex
    unreachableReplicas map[string]unreachableReplica
}

type unreachableReplica struct {
    failedAt time.Time
    err      error
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

/* Reader falls back to the primary only for an unreachable replica; every other failure is refused. */
func (instance *ReadWriteSplitter) Reader() (*bun.DB, error) {
    readerName := instance.ReaderName()

    if readerName != instance.primaryName {
        if rememberedErr := instance.rememberedUnreachable(readerName); nil != rememberedErr {
            return instance.primaryInPlaceOf(readerName, rememberedErr)
        }
    }

    database, databaseErr := instance.registry.Database(readerName)
    if nil == databaseErr {
        if readerName != instance.primaryName {
            instance.forgetUnreachable(readerName)
        }

        return database, nil
    }

    if false == exception.ChainIs(databaseErr, ErrDatabaseUnreachable) {
        return nil, databaseErr
    }

    if readerName == instance.primaryName {
        return nil, databaseErr
    }

    instance.rememberUnreachable(readerName, databaseErr)

    return instance.primaryInPlaceOf(readerName, databaseErr)
}

func (instance *ReadWriteSplitter) primaryInPlaceOf(readerName string, replicaErr error) (*bun.DB, error) {
    primary, primaryErr := instance.registry.Database(instance.primaryName)
    if nil != primaryErr {
        return nil, keepLoggedMark(exception.NewError(
            "read/write splitter could not open the replica nor the primary",
            map[string]any{
                "replica":      readerName,
                "primary":      instance.primaryName,
                "replicaError": replicaErr.Error(),
            },
            primaryErr,
        ), primaryErr)
    }

    return primary, nil
}

func (instance *ReadWriteSplitter) rememberedUnreachable(replicaName string) error {
    instance.unreachableMutex.Lock()
    defer instance.unreachableMutex.Unlock()

    remembered, isRemembered := instance.unreachableReplicas[replicaName]
    if false == isRemembered || instance.replicaRetryInterval <= instance.now().Sub(remembered.failedAt) {
        return nil
    }

    return remembered.err
}

func (instance *ReadWriteSplitter) rememberUnreachable(replicaName string, replicaErr error) {
    instance.unreachableMutex.Lock()
    _, wasRemembered := instance.unreachableReplicas[replicaName]
    instance.unreachableReplicas[replicaName] = unreachableReplica{failedAt: instance.now(), err: replicaErr}
    instance.unreachableMutex.Unlock()

    if true == wasRemembered {
        return
    }

    instance.registry.currentLogger().Warning(
        "read replica unreachable, reads served by the primary",
        map[string]any{
            "replica":    replicaName,
            "primary":    instance.primaryName,
            "retryAfter": instance.replicaRetryInterval.String(),
            "error":      replicaErr.Error(),
        },
    )
}

func (instance *ReadWriteSplitter) forgetUnreachable(replicaName string) {
    instance.unreachableMutex.Lock()
    _, wasRemembered := instance.unreachableReplicas[replicaName]
    delete(instance.unreachableReplicas, replicaName)
    instance.unreachableMutex.Unlock()

    if false == wasRemembered {
        return
    }

    instance.registry.currentLogger().Info(
        "read replica reachable again, reads served by it",
        map[string]any{"replica": replicaName, "primary": instance.primaryName},
    )
}
