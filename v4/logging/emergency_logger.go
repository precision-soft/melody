package logging

import (
    "context"
    "os"
    "sync"

    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

var (
    emergencyLoggerMutex    sync.Mutex
    emergencyLoggerInstance loggingcontract.Logger
)

func EmergencyLogger() loggingcontract.Logger {
    emergencyLoggerMutex.Lock()
    defer emergencyLoggerMutex.Unlock()

    if nil == emergencyLoggerInstance {
        emergencyLoggerInstance = NewJsonLogger(os.Stderr, JsonLoggerConfig{})
    }

    return emergencyLoggerInstance
}

func CloseEmergencyLogger() {
    emergencyLoggerMutex.Lock()
    defer emergencyLoggerMutex.Unlock()

    if nil == emergencyLoggerInstance {
        return
    }

    if closer, isCloser := emergencyLoggerInstance.(interface {
        CloseWithContext(closeContext context.Context) error
    }); true == isCloser {
        _ = closer.CloseWithContext(context.Background())
    }

    emergencyLoggerInstance = nil
}
