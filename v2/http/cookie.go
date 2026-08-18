package http

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

func SetCookie(response httpcontract.Response, cookie *nethttp.Cookie) {
    if "" == cookie.Name {
        exception.Panic(
            exception.NewError("the cookie name is empty and can not be set", nil, nil),
        )
    }

    /* nil headers are a state the contract permits — SetHeaders stores the nil it is given — and every other
       consumer of Headers() in the chain checks for it before writing. This one wrote into a nil map, which on
       the session paths is a panic on a response the handler had already produced, and on the panic-recovery
       path a second panic after recover has run. */
    if nil == response.Headers() {
        response.SetHeaders(make(nethttp.Header))
    }

    response.Headers().Add("Set-Cookie", cookie.String())
}

func DeleteCookie(response httpcontract.Response, name string, path string) {
    if "" == name {
        exception.Panic(
            exception.NewError("the cookie name is empty and can not be deleted", nil, nil),
        )
    }

    if "" == path {
        path = "/"
    }

    cookie := &nethttp.Cookie{
        Name:     name,
        Value:    "",
        Path:     path,
        MaxAge:   -1,
        HttpOnly: true,
    }

    SetCookie(response, cookie)
}
