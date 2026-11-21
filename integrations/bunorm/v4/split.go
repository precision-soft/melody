package bunorm

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* SplitterConfig declares the replicas a splitter reads from and how long an unreachable one is remembered: ReplicaNames in the order the reads rotate over, none routing every read to the primary, an empty name refused by its index; ReplicaRetryInterval the time a replica whose open failed as unreachable is skipped before it is dialled again, 5s when left out, at least 1ms, unlimited refused, since a replica never dialled again is a silent primary-only split. */
type SplitterConfig struct {
    ReplicaNames         []string
    ReplicaRetryInterval bound.Duration
}

/* long enough that a dead replica does not cost every read a connect timeout, short enough that a recovered one takes reads back promptly */
var splitterReplicaRetryIntervalRule = bound.Rule[time.Duration]{
    Name:    "SplitterConfig.ReplicaRetryInterval",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

/* NewReadWriteSplitter routes writes to the primary and reads round-robin over the replicas; a nil registry, an empty primary name, an empty replica name and a ReplicaRetryInterval outside its rule panic, the last naming the field. */
func NewReadWriteSplitter(registry *ManagerRegistry, primaryName string, config SplitterConfig) *ReadWriteSplitter {
    if nil == registry {
        exception.Panic(exception.NewError("read/write splitter registry is nil", nil, nil))
    }

    if "" == primaryName {
        exception.Panic(exception.NewError("read/write splitter primary name is empty", nil, nil))
    }

    /* an empty replica name would fail every resolution it is picked for, and the fallback below would route that share of the reads to the primary forever with no signal; it is a wiring error and is refused where it is written */
    for index, replicaName := range config.ReplicaNames {
        if "" == replicaName {
            exception.Panic(exception.NewError("read/write splitter replica name is empty", map[string]any{"index": index}, nil))
        }
    }

    replicaRetryInterval, replicaRetryIntervalErr := bound.Resolve(config.ReplicaRetryInterval, splitterReplicaRetryIntervalRule)
    if nil != replicaRetryIntervalErr {
        exception.Panic(exception.FromError(replicaRetryIntervalErr))
    }

    return &ReadWriteSplitter{
        registry:             registry,
        primaryName:          primaryName,
        replicaNames:         append([]string{}, config.ReplicaNames...),
        replicaRetryInterval: replicaRetryInterval.Value,
        now:                  time.Now,
        unreachableReplicas:  map[string]unreachableReplica{},
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

/* Writer answers the primary, opened under the caller's runtime as the registry's Database opens it. */
func (instance *ReadWriteSplitter) Writer(runtimeInstance containercontract.Runtime) (*bun.DB, error) {
    return instance.registry.Database(runtimeInstance, instance.WriterName())
}

/* Reader answers the round-robin replica, and falls back to the primary only when the replica is unreachable, an open failure filed under ErrDatabaseUnreachable. Every other failure, a wiring error or a teardown in progress, is refused, so a dead replica configuration is never served silently from the primary. An unreachable replica is remembered for the retry interval, so its reads go to the primary without dialling it again, and the change is journaled once each way: a warning when reads move to the primary, an info when the replica takes them back. When the primary fails too, the answer carries its failure as the cause and names the replica failure beside it. Every open runs under the caller's runtime, and a replica open that the caller's own context ended is answered as that refusal: the caller left, so the replica is not remembered unreachable and the primary is not opened in its place. */
func (instance *ReadWriteSplitter) Reader(runtimeInstance containercontract.Runtime) (*bun.DB, error) {
    readerName := instance.ReaderName()

    if readerName != instance.primaryName {
        if rememberedErr := instance.rememberedUnreachable(readerName); nil != rememberedErr {
            return instance.primaryInPlaceOf(runtimeInstance, readerName, rememberedErr)
        }
    }

    database, databaseErr := instance.registry.Database(runtimeInstance, readerName)
    if nil == databaseErr {
        if readerName != instance.primaryName {
            instance.forgetUnreachable(readerName)
        }

        return database, nil
    }

    if true == endedByCallerContext(runtimeInstance, databaseErr) {
        return nil, databaseErr
    }

    if false == exception.ChainIs(databaseErr, ErrDatabaseUnreachable) {
        return nil, databaseErr
    }

    if readerName == instance.primaryName {
        return nil, databaseErr
    }

    instance.rememberUnreachable(readerName, databaseErr)

    return instance.primaryInPlaceOf(runtimeInstance, readerName, databaseErr)
}

/* endedByCallerContext answers whether a failure is the caller's own departure: its context is done and the failure carries that context's error or cause */
func endedByCallerContext(runtimeInstance containercontract.Runtime, failure error) bool {
    if true == isNilInterface(runtimeInstance) || true == isNilInterface(runtimeInstance.Context()) {
        return false
    }

    callerContext := runtimeInstance.Context()
    if nil == callerContext.Err() {
        return false
    }

    return true == exception.ChainIs(failure, callerContext.Err()) || true == exception.ChainIs(failure, context.Cause(callerContext))
}

func (instance *ReadWriteSplitter) primaryInPlaceOf(runtimeInstance containercontract.Runtime, readerName string, replicaErr error) (*bun.DB, error) {
    primary, primaryErr := instance.registry.Database(runtimeInstance, instance.primaryName)
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

/* rememberedUnreachable answers the failure of a replica still inside its retry interval, and nil for a replica to dial, once per lapsed interval */
func (instance *ReadWriteSplitter) rememberedUnreachable(replicaName string) error {
    instance.unreachableMutex.Lock()
    defer instance.unreachableMutex.Unlock()

    remembered, isRemembered := instance.unreachableReplicas[replicaName]
    if false == isRemembered {
        return nil
    }

    now := instance.now()
    if instance.replicaRetryInterval <= now.Sub(remembered.failedAt) {
        /* the reader that finds the interval lapsed dials alone: the failure is dated again, so the readers beside it keep the primary instead of waiting on its open */
        remembered.failedAt = now
        instance.unreachableReplicas[replicaName] = remembered

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

    instance.registry.logger.Warning(
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

    instance.registry.logger.Info(
        "read replica reachable again, reads served by it",
        map[string]any{"replica": replicaName, "primary": instance.primaryName},
    )
}
