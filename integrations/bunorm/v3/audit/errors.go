package audit

import (
    "errors"
    "reflect"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* ErrAsyncStorageQueueFull and ErrAsyncStorageClosed are what AsyncStorage.Save returns for an entry it could not take: the buffer was full, or the storage was closed. The storage has dead-lettered such an entry through its own logger before returning; a Recorder that reads the refusal journals the entry itself unless the storage journaled it through the recorder's own logger, so a lost entry reaches the application's journal in every assembly and reaches no journal twice. */
var ErrAsyncStorageQueueFull = errors.New("async audit storage queue is full")

var ErrAsyncStorageClosed = errors.New("async audit storage is closed")

/* journaledRefusal is a refusal the async storage has already dead-lettered, carrying the logger it went through, so the recorder journals the entry itself only when that logger is not its own. It unwraps to the refusal, so errors.Is against the sentinels and errors.As against the exception keep their answers. */
type journaledRefusal struct {
    sentinel error
    journal  loggingcontract.Logger
}

/* Error renders the sentinel alone, and Is answers for it in place of an Unwrap, so a journal that renders the cause chain reads the sentinel once. errors.As still finds the refusal through the exception's own unwrap. */
func (instance *journaledRefusal) Error() string {
    return instance.sentinel.Error()
}

func (instance *journaledRefusal) Is(target error) bool {
    return target == instance.sentinel
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
