package contract

import (
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

type LoginInput struct {
	Token Token
}

type LoginResult struct {
	Token    Token
	Response httpcontract.Response
}
