package contract

import (
    "os"

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

    /* LogFileMode is MELODY_LOG_FILE_MODE, the permission the journal file is created with: 0640 by default. */
    LogFileMode() os.FileMode

    /* LogDirectoryMode is MELODY_LOG_DIRECTORY_MODE, the permission the logs directory and every missing directory of MELODY_LOG_PATH are created with: 0750 by default. */
    LogDirectoryMode() os.FileMode

    /* CacheDirectoryMode is MELODY_CACHE_DIRECTORY_MODE, the permission the cache directory the boot creates is created with: 0750 by default. */
    CacheDirectoryMode() os.FileMode

    /* BootWarningsIgnored is MELODY_BOOT_WARNINGS_IGNORED, the boot warning codes the process, http or cli, leaves out of its journal: none by default. */
    BootWarningsIgnored() []string

    /* TeardownTimeout is MELODY_TEARDOWN_TIMEOUT as the deployment set it, absent when the key says nothing: the application decides it against its own config, the key winning. */
    TeardownTimeout() bound.Duration

    /* ScopeTeardownTimeout is MELODY_SCOPE_TEARDOWN_TIMEOUT as the deployment set it, absent when the key says nothing: the application decides it against its own config, the key winning. */
    ScopeTeardownTimeout() bound.Duration

    /* ContainerCreationWaiters is MELODY_CONTAINER_CREATION_WAITERS as the deployment set it, absent when the key says nothing: the application decides it against its own config, the key winning. */
    ContainerCreationWaiters() bound.Count

    /* ContainerTeardownSequential is MELODY_CONTAINER_TEARDOWN_SEQUENTIAL as the deployment set it, unset when the key says nothing: the application decides it against its own config, the key winning. */
    ContainerTeardownSequential() Switch

    /* Namespace is the application namespace every store family derives its default names from: MELODY_NAMESPACE as the deployment set it, else the application's own, else melody. */
    Namespace() string
}

/* Switch is a boolean key as the deployment set it: IsSet is false when the key says nothing, and Value is then false. */
type Switch struct {
    Value bool
    IsSet bool
}
