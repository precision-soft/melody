package contract

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
)

type LoginInput struct {
    Token Token
}

type LoginResult struct {
    Token    Token
    Response httpcontract.Response
}
