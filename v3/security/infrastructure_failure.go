package security

import (
    "errors"
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

func markInfrastructureFailure(err error) error {
    if nil == err {
        return nil
    }

    return &infrastructureFailure{cause: err}
}

func isInfrastructureFailure(err error) bool {
    var marker *infrastructureFailure

    return errors.As(err, &marker)
}
