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

    /* nil headers are a state the contract permits (SetHeaders stores the nil it is given), so they are checked before writing: a write into a nil map would panic on a response the handler already produced, a second panic on the recovery path. */
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
