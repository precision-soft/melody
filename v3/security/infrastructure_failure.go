package security

import (
    "github.com/precision-soft/melody/v3/exception"
)

type infrastructureFailure struct {
    cause error
}

func (instance *infrastructureFailure) Error() string {
    return instance.cause.Error()
}

func (instance *infrastructureFailure) Unwrap() error {
    return instance.cause
}

/* MarkInfrastructureFailure files err as a platform failure, logged at Error rather than Info; a nil error stays nil */
func MarkInfrastructureFailure(err error) error {
    if nil == err {
        return nil
    }

    return &infrastructureFailure{cause: err}
}

func IsInfrastructureFailure(err error) bool {
    var marker *infrastructureFailure

    return exception.ChainAs(err, &marker)
}
