/* Package httpkernelseam is the door the application installs its middleware pipeline on the http kernel through; a Token is valid only when NewToken built it, which code outside v4 cannot call, and the door refuses any other. */
package httpkernelseam

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type Token struct {
    valid bool
}

func NewToken() Token {
    return Token{valid: true}
}

/* Valid answers whether NewToken built the token; the zero Token, the only one code outside v4 can make, is not. */
func (instance Token) Valid() bool {
    return instance.valid
}

type MiddlewareInstaller interface {
    InstallMiddlewares(token Token, middlewares []httpcontract.Middleware)
}
