package security

import (
    "errors"
)

/* infrastructureFailure marks an error whose cause is the platform the security machinery stands on — a nonce guard that could not answer, a revocation epoch store that is down, an enrichment lookup that failed — as opposed to a credential that failed its checks. The token sources fail CLOSED on both (the request proceeds anonymous either way), but the two must not share a log severity: a forged signature rejected at Info is routine noise, while a fleet silently degrading every caller to anonymous because its shared backend is down is an incident nothing else will report. The mark travels as a link in the cause chain, so wrapping the marked error in further melody errors keeps it readable through errors.As. */
type infrastructureFailure struct {
    cause error
}

func (instance *infrastructureFailure) Error() string {
    return instance.cause.Error()
}

func (instance *infrastructureFailure) Unwrap() error {
    return instance.cause
}

/* markInfrastructureFailure marks err as an infrastructure failure; a nil error stays nil. */
func markInfrastructureFailure(err error) error {
    if nil == err {
        return nil
    }

    return &infrastructureFailure{cause: err}
}

/* isInfrastructureFailure reports whether the chain of err carries the infrastructure mark. */
func isInfrastructureFailure(err error) bool {
    var marker *infrastructureFailure

    return errors.As(err, &marker)
}
