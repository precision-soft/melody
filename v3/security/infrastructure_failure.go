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

/* MarkInfrastructureFailure marks err as a failure of the platform the security machinery stands on, a store or a remote that could not answer, rather than a credential that failed its checks. An application's TokenValidator, ClaimsEnricher or resolver returns its error through it so the token sources file the failure as an incident, at Error, where an unmarked error is the routine Info a bad credential earns; the request fails closed to anonymous either way. A nil error stays nil. */
func MarkInfrastructureFailure(err error) error {
    if nil == err {
        return nil
    }

    return &infrastructureFailure{cause: err}
}

/* IsInfrastructureFailure reports whether a link of err's cause chain carries the mark of MarkInfrastructureFailure */
func IsInfrastructureFailure(err error) bool {
    var marker *infrastructureFailure

    return errors.As(err, &marker)
}
