package bunorm

import (
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
    counter      uint64
}

func (instance *ReadWriteSplitter) WriterName() string {
    return instance.primaryName
}

func (instance *ReadWriteSplitter) ReaderName() string {
    if 0 == len(instance.replicaNames) {
        return instance.primaryName
    }

    index := atomic.AddUint64(&instance.counter, 1)
    return instance.replicaNames[(index-1)%uint64(len(instance.replicaNames))]
}

func (instance *ReadWriteSplitter) Writer() (*bun.DB, error) {
    return instance.registry.Database(instance.WriterName())
}

func (instance *ReadWriteSplitter) Reader() (*bun.DB, error) {
    database, databaseErr := instance.registry.Database(instance.ReaderName())
    if nil != databaseErr {
        return instance.registry.Database(instance.primaryName)
    }

    return database, nil
}
