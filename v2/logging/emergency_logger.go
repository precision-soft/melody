package logging

import (
    "os"
    "sync"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

var (
    emergencyLoggerMutex    sync.Mutex
    emergencyLoggerInstance loggingcontract.Logger
)

func EmergencyLogger() loggingcontract.Logger {
    emergencyLoggerMutex.Lock()
    defer emergencyLoggerMutex.Unlock()

    if nil == emergencyLoggerInstance {
        emergencyLoggerInstance = NewJsonLogger(os.Stderr, loggingcontract.LevelInfo)
    }

    return emergencyLoggerInstance
}

func CloseEmergencyLogger() {
    emergencyLoggerMutex.Lock()
    defer emergencyLoggerMutex.Unlock()

    if nil == emergencyLoggerInstance {
        return
    }

    if closer, isCloser := emergencyLoggerInstance.(interface{ Close() error }); true == isCloser {
        _ = closer.Close()
    }

    emergencyLoggerInstance = nil
}
