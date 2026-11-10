package security

import (
    "github.com/precision-soft/melody/v4/exception"
)

/* infrastructureFailure marks an error caused by the platform the security machinery stands on, as opposed to a credential that failed its checks: a platform failure answers the unavailable token and is recorded as an incident, a failed credential answers anonymous. The mark is a link in the cause chain, readable through errors.As. */
type infrastructureFailure struct {
    cause error
}

func (instance *infrastructureFailure) Error() string {
    return instance.cause.Error()
}

func (instance *infrastructureFailure) Unwrap() error {
    return instance.cause
}

/* MarkInfrastructureFailure marks err as a failure of the platform the security machinery stands on, a store or a remote that could not answer, rather than a credential that failed its checks. An application's TokenValidator, TokenEnricher or ImpersonatedUserResolver returns its error through it so the token sources answer the unavailable token for it, 503 with Retry-After on a protected route and anonymous on a public one, recorded at Error by the security resolution listener, where an unmarked error is the anonymous token and the routine Info a bad credential earns. A token source returning a marked error from Resolve, and an Authenticator returning one, are read the same way, unless an http status answers outside what the mark wraps, which is kept. A nil error stays nil. */
func MarkInfrastructureFailure(err error) error {
    if nil == err {
        return nil
    }

    return &infrastructureFailure{cause: err}
}

/* answeredAsUnavailable reports whether err is answered as the unavailable token: it carries the mark and no http status answers outside what the mark wraps, so an application's own answer around a marked cause is kept and a store's status beneath the mark is not served to the caller. */
func answeredAsUnavailable(err error) bool {
    var marker *infrastructureFailure
    if false == exception.ChainAs(err, &marker) || nil == marker {
        return false
    }

    answer := exception.AsHttpException(err)

    return nil == answer || answer == exception.AsHttpException(marker)
}

/* IsInfrastructureFailure reports whether a link of err's cause chain carries the mark of MarkInfrastructureFailure */
func IsInfrastructureFailure(err error) bool {
    var marker *infrastructureFailure

    return exception.ChainAs(err, &marker)
}
