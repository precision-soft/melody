package audit

import (
    "errors"
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

var ErrAsyncStorageQueueFull = errors.New("async audit storage queue is full")

var ErrAsyncStorageClosed = errors.New("async audit storage is closed")

type journaledRefusal struct {
    sentinel error
    journal  loggingcontract.Logger
}

/* Error renders the sentinel alone, and Is answers for it in place of an Unwrap, so a cause chain reads the sentinel once */
func (instance *journaledRefusal) Error() string {
    return instance.sentinel.Error()
}

func (instance *journaledRefusal) Is(target error) bool {
    return target == instance.sentinel
}

/* comparing two interface values holding a slice or a map panics; a logger without identity reads as a different journal */
func journaledThrough(saveErr error, logger loggingcontract.Logger) bool {
    var refusal *journaledRefusal
    if false == exception.ChainAs(saveErr, &refusal) {
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
