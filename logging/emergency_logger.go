package logging

import (
	"os"
	"sync"

	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

var (
	emergencyLoggerOnce     sync.Once
	emergencyLoggerInstance loggingcontract.Logger
)

func EmergencyLogger() loggingcontract.Logger {
	emergencyLoggerOnce.Do(func() {
		emergencyLoggerInstance = NewJsonLogger(os.Stderr, loggingcontract.LevelInfo)
	})

	return emergencyLoggerInstance
}

func CloseEmergencyLogger() {
	if nil == emergencyLoggerInstance {
		return
	}

	if closer, isCloser := emergencyLoggerInstance.(interface{ Close() error }); true == isCloser {
		_ = closer.Close()
	}
}
