package contract

import (
    "github.com/precision-soft/melody/v4/bound"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type KernelConfiguration interface {
    DefaultMode() string

    ProcessRole() string

    Environment() string

    ProjectDirectory() string

    LogsDirectory() string

    CacheDirectory() string

    LogPath() string

    LogLevel() loggingcontract.Level

    /* TeardownTimeout is MELODY_TEARDOWN_TIMEOUT as the deployment set it, absent when the key says nothing: the application decides it against its own config, the key winning. */
    TeardownTimeout() bound.Duration

    /* ScopeTeardownTimeout is MELODY_SCOPE_TEARDOWN_TIMEOUT as the deployment set it, absent when the key says nothing: the application decides it against its own config, the key winning. */
    ScopeTeardownTimeout() bound.Duration

    /* ContainerCreationWaiters is MELODY_CONTAINER_CREATION_WAITERS as the deployment set it, absent when the key says nothing: the application decides it against its own config, the key winning. */
    ContainerCreationWaiters() bound.Count

    /* ContainerTeardownSequential is MELODY_CONTAINER_TEARDOWN_SEQUENTIAL as the deployment set it, unset when the key says nothing: the application decides it against its own config, the key winning. */
    ContainerTeardownSequential() Switch
}

/* Switch is a boolean key as the deployment set it: IsSet is false when the key says nothing, and Value is then false. */
type Switch struct {
    Value bool
    IsSet bool
}
