package exception

/* infrastructureFailure marks an error caused by the platform a layer stands on, a store or a remote that could not answer, as opposed to a request or a credential that failed its checks. The mark is a link in the cause chain, readable through errors.As. */
type infrastructureFailure struct {
    cause error
}

func (instance *infrastructureFailure) Error() string {
    return instance.cause.Error()
}

func (instance *infrastructureFailure) Unwrap() error {
    return instance.cause
}

/* MarkInfrastructureFailure marks err as a failure of the platform a layer stands on, a store or a remote that could not answer, rather than a request or a credential that failed its checks. The error renderer answers a marked error 503 with Retry-After where an unmarked one is a 500, from any layer: an application's storage, cache or handler. The security machinery reads the same mark: a TokenValidator, TokenEnricher, ImpersonatedUserResolver, token source or Authenticator returning a marked error answers the unavailable token, 503 with the firewall's Retry-After on a protected route and anonymous on a public one, where an unmarked error is the anonymous token. An http status answering outside what the mark wraps is kept. A nil error stays nil. */
func MarkInfrastructureFailure(err error) error {
    if nil == err {
        return nil
    }

    return &infrastructureFailure{cause: err}
}

/* IsInfrastructureFailure reports whether a link of err's cause chain carries the mark of MarkInfrastructureFailure */
func IsInfrastructureFailure(err error) bool {
    var marker *infrastructureFailure

    return ChainAs(err, &marker)
}

/* AnswersUnavailable reports whether err is answered as unavailable: it carries the mark and no http status answers outside what the mark wraps, so an application's own answer around a marked cause is kept and a store's status beneath the mark is not served to the caller. */
func AnswersUnavailable(err error) bool {
    var marker *infrastructureFailure
    if false == ChainAs(err, &marker) || nil == marker {
        return false
    }

    answer := AsHttpException(err)

    return nil == answer || answer == AsHttpException(marker)
}
