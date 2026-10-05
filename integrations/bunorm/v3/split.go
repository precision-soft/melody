package bunorm

import (
    "errors"
    "sync"
    "sync/atomic"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
)

/* NewReadWriteSplitter routes writes to the primary and reads round-robin over the replicas, with the defaults of NewReadWriteSplitterWithOptions. */
func NewReadWriteSplitter(registry *ManagerRegistry, primaryName string, replicaNames ...string) *ReadWriteSplitter {
    return NewReadWriteSplitterWithOptions(registry, primaryName, replicaNames)
}

/* NewReadWriteSplitterWithOptions is NewReadWriteSplitter configured by options: WithReplicaRetryInterval sets how long an unreachable replica is remembered before it is dialled again. */
func NewReadWriteSplitterWithOptions(registry *ManagerRegistry, primaryName string, replicaNames []string, options ...ReadWriteSplitterOption) *ReadWriteSplitter {
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

/* ReadWriteSplitterOption configures a splitter built through NewReadWriteSplitterWithOptions. */
type ReadWriteSplitterOption func(splitter *ReadWriteSplitter)

/* defaultReplicaRetryInterval is how long an unreachable replica is remembered by default: long enough that a dead replica does not cost every read a connect timeout, short enough that a recovered one takes reads back promptly. */
const defaultReplicaRetryInterval = 5 * time.Second

/* WithReplicaRetryInterval sets how long a replica whose open failed as unreachable is remembered: inside the interval its reads go to the primary without dialling it, and the first read after the interval dials it again. A zero interval keeps the default of five seconds, and a negative one is a wiring error, refused where it is written. */
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

    /* the replicas whose last open failed as unreachable, read and written under unreachableMutex; an entry is the instant of the failure and the failure itself */
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

/* Reader answers the round-robin replica, and falls back to the primary only when the replica is unreachable, an open failure filed under ErrDatabaseUnreachable. Every other failure, a wiring error or a teardown in progress, is refused, so a dead replica configuration is never served silently from the primary. An unreachable replica is remembered for the retry interval, so its reads go to the primary without dialling it again, and the change is journaled once each way: a warning when reads move to the primary, an info when the replica takes them back. When the primary fails too, the answer carries its failure as the cause and names the replica failure beside it. */
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

    if false == errors.Is(databaseErr, ErrDatabaseUnreachable) {
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

/* rememberedUnreachable answers the failure of a replica still inside its retry interval, and nil for a replica to dial */
func (instance *ReadWriteSplitter) rememberedUnreachable(replicaName string) error {
    instance.unreachableMutex.Lock()
    defer instance.unreachableMutex.Unlock()

    remembered, isRemembered := instance.unreachableReplicas[replicaName]
    if false == isRemembered || instance.replicaRetryInterval <= instance.now().Sub(remembered.failedAt) {
        return nil
    }

    return remembered.err
}

/* rememberUnreachable records the replica's failure and journals the move of its reads to the primary when it was not already remembered */
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

/* forgetUnreachable drops a replica that answered again and journals that it takes its reads back */
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
