package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type LogoutInput struct {
    /* Token is whom the request signs out, anonymous when nobody is signed in. */
    Token Token
}

type LogoutResult struct {
    Response httpcontract.Response
}
