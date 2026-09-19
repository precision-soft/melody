package bunorm

import "errors"

var ErrNoProviderDefinitions = errors.New("no provider definitions are registered")

var ErrMultipleDefaultProviderDefinitions = errors.New("multiple provider definitions are marked as default")

var ErrProviderDefinitionNameIsRequired = errors.New("provider definition name is required")

var ErrProviderDefinitionNameMustBeUnique = errors.New("provider definition name must be unique")

var ErrProviderDefinitionNotFound = errors.New("provider definition not found")

var ErrProviderIsRequired = errors.New("provider is required")

var ErrLoggerIsRequired = errors.New("logger is required")

var ErrManagerRegistryClosed = errors.New("manager registry is closed")

var ErrProviderReturnedNilDatabase = errors.New("provider returned neither a database nor an error")

/* ErrDatabaseUnreachable is the class of an open failure the provider classified as TRANSIENT — a dial refused, a name that would not resolve, a timeout — and could not get past: the outage a ReadWriteSplitter absorbs by reading from the primary. Every other open failure is terminal by nature — a parameter the provider refused, a password the server refused, a database that does not exist — and reaches the caller as it is, so a replica whose configuration is wrong is refused instead of served silently from the primary. A provider that does not mark its failures has them read as terminal, which is the direction that surfaces a misconfiguration. */
var ErrDatabaseUnreachable = errors.New("database unreachable")

/* DatabaseUnreachable files an open failure under ErrDatabaseUnreachable. The link sits under the provider's exception as its cause — the exception keeps its message and its context — and unwraps to the failure the provider saw, so errors.Is answers the class, errors.As still reaches the driver error, and a journal that renders the cause chain reads the class once, where the driver failure stood. */
func DatabaseUnreachable(openErr error) error {
    return &unreachableDatabase{openErr: openErr}
}

type unreachableDatabase struct {
    openErr error
}

func (instance *unreachableDatabase) Error() string {
    return ErrDatabaseUnreachable.Error()
}

func (instance *unreachableDatabase) Is(target error) bool {
    return target == ErrDatabaseUnreachable
}

func (instance *unreachableDatabase) Unwrap() error {
    return instance.openErr
}
