package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* LoginHandler signs the request in at the firewall's sign-in door; an error it returns raises security.login.failure only when it answers 401 or 403, and an error carrying exception.MarkInfrastructureFailure is answered 503 with the firewall's Retry-After. */
type LoginHandler interface {
    Login(runtimeInstance containercontract.Runtime, request httpcontract.Request, input LoginInput) (*LoginResult, error)
}
