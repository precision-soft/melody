package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type LoginInput struct {
    Token Token

    /* PreviousSessionId is the id the framework's rotation retired, for a handler keeping an index of sessions; empty when the door has no authenticator and the handler rotates itself. */
    PreviousSessionId string
}

type LoginResult struct {
    Token    Token
    Response httpcontract.Response
}
