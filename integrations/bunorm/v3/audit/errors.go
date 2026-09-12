package audit

import (
    "errors"
    "reflect"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* ErrAsyncStorageQueueFull and ErrAsyncStorageClosed are what AsyncStorage.Save returns for an entry it could not take: the buffer was full, or the storage was closed. The storage has dead-lettered such an entry through its own logger before returning; a Recorder that reads the refusal journals the entry itself unless the storage journaled it through the recorder's own logger, so a lost entry reaches the application's journal in every assembly and reaches no journal twice. */
var ErrAsyncStorageQueueFull = errors.New("async audit storage queue is full")

var ErrAsyncStorageClosed = errors.New("async audit storage is closed")

/* journaledRefusal is a refusal the async storage has already dead-lettered, carrying the logger it did so through. It is what lets the recorder decide whether its own dead-letter would be a second record in the same journal or the only record in the application's: skipped on the sentinel alone, the recorder's record was lost whenever the storage journaled through the emergency default and the recorder through the application's logger — the wiring the readme describes. It unwraps to the refusal it carries, so errors.Is against the sentinels and errors.As against the exception keep their answers. */
type journaledRefusal struct {
    error
    journal loggingcontract.Logger
}

func (instance *journaledRefusal) Unwrap() error {
    return instance.error
}

/* journaledThrough answers whether a refusal was already dead-lettered through the given logger. Identity is asked only of loggers whose dynamic type can carry one — a comparison of two interface values holding a slice or a map is a runtime panic — and a logger without identity is read as a different journal, so the entry is journaled by both rather than by neither. */
func journaledThrough(saveErr error, logger loggingcontract.Logger) bool {
    var refusal *journaledRefusal
    if false == errors.As(saveErr, &refusal) {
        return false
    }

    if nil == refusal.journal || nil == logger {
        return false
    }

    if false == reflect.ValueOf(refusal.journal).Comparable() || false == reflect.ValueOf(logger).Comparable() {
        return false
    }

    return refusal.journal == logger
}
