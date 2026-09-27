package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type LogoutInput struct{}

type LogoutResult struct {
    Response httpcontract.Response
}
