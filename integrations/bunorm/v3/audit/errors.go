package audit

import (
    "errors"
    "reflect"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* ErrAsyncStorageQueueFull and ErrAsyncStorageClosed identify entries AsyncStorage.Save cannot queue. The refusal records the dead-letter logger so Recorder can avoid reporting the same loss twice through that logger. */
var ErrAsyncStorageQueueFull = errors.New("async audit storage queue is full")

var ErrAsyncStorageClosed = errors.New("async audit storage is closed")

type journaledRefusal struct {
    sentinel error
    journal  loggingcontract.Logger
}

/* Error renders only the sentinel. Is supports errors.Is without an additional cause-chain link; errors.As can still reach the refusal through its enclosing exception. */
func (instance *journaledRefusal) Error() string {
    return instance.sentinel.Error()
}

func (instance *journaledRefusal) Is(target error) bool {
    return target == instance.sentinel
}

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
