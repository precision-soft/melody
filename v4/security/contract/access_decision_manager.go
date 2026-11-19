package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* AccessDecisionManager decides whether a token holds the attributes for a subject: nil grants, an error refuses, and an error exception.AnswersUnavailable reads as unavailable is a decision that could not be made, answered 503 with the firewall's Retry-After and the access denied handler not consulted. */
type AccessDecisionManager interface {
    DecideAll(runtimeInstance runtimecontract.Runtime, token Token, attributes []string, subject any) error

    DecideAny(runtimeInstance runtimecontract.Runtime, token Token, attributes []string, subject any) error
}
