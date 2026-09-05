package audit

import "errors"

var ErrAsyncStorageQueueFull = errors.New("async audit storage queue is full")

var ErrAsyncStorageClosed = errors.New("async audit storage is closed")
