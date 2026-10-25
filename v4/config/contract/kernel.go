package contract

import (
    "github.com/precision-soft/melody/v4/bound"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type KernelConfiguration interface {
    DefaultMode() string

    ProcessRole() string

    Env() string

    ProjectDir() string

    LogsDir() string

    CacheDir() string

    LogPath() string

    LogLevel() loggingcontract.Level

    /* TeardownTimeout is MELODY_TEARDOWN_TIMEOUT as the deployment set it, absent when the key says nothing: the application decides it against its own config, the key winning. */
    TeardownTimeout() bound.Duration
}
