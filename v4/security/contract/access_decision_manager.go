package contract

/* AccessDecisionManager decides whether a token holds the attributes for a subject: nil grants, an error refuses, and an error exception.AnswersUnavailable reads as unavailable is a decision that could not be made, answered 503 with the firewall's Retry-After and the access denied handler not consulted. */
type AccessDecisionManager interface {
    DecideAll(token Token, attributes []string, subject any) error

    DecideAny(token Token, attributes []string, subject any) error
}
