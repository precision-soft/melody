package http

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
)

func SetCookie(response httpcontract.Response, cookie *nethttp.Cookie) {
    if "" == cookie.Name {
        exception.Panic(
            exception.NewError("the cookie name is empty and can not be set", nil, nil),
        )
    }

    /* the standard library serializes a cookie whose name is not a valid token as the empty string, which would be written as an empty Set-Cookie header */
    serialized := cookie.String()
    if "" == serialized {
        exception.Panic(
            exception.NewError("the cookie name is invalid and can not be set", map[string]any{"name": cookie.Name}, nil),
        )
    }

    /* nil headers are a state the contract permits (SetHeaders stores the nil it is given), so they are checked before writing: a write into a nil map would panic on a response the handler already produced, a second panic on the recovery path. */
    if nil == response.Headers() {
        response.SetHeaders(make(nethttp.Header))
    }

    response.Headers().Add("Set-Cookie", serialized)
}

/* DeleteCookie writes the expiring counterpart of the named host-only cookie; an empty path reads as "/". A cookie set with a Domain is matched by the browser on its domain and is not reached: write its expiring counterpart, with the same Domain, through SetCookie. */
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
