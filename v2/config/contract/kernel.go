package contract

import (
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type KernelConfiguration interface {
    DefaultMode() string

    Env() string

    ProjectDir() string

    LogsDir() string

    CacheDir() string

    LogPath() string

    LogLevel() loggingcontract.Level
}
