package security

import (
    "errors"
)

/* infrastructureFailure marks an error caused by the platform the security machinery stands on, as opposed to a credential that failed its checks. Both fail closed to anonymous, but a platform failure is logged as an incident. The mark is a link in the cause chain, readable through errors.As. */
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
