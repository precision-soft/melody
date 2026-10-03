package http

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* SetCookie adds the cookie to the response and leaves Cache-Control alone: whether a response addresses one client is the caller's decision. The session path marks the session cookie's response private itself. */
func SetCookie(response httpcontract.Response, cookie *nethttp.Cookie) {
    if "" == cookie.Name {
        exception.Panic(
            exception.NewError("the cookie name is empty and can not be set", nil, nil),
        )
    }

    /* nil headers are a state the contract permits, so the map is created here */
    if nil == response.Headers() {
        response.SetHeaders(make(nethttp.Header))
    }

    response.Headers().Add("Set-Cookie", cookie.String())
}

/* DeleteCookie writes the expiring counterpart of the named cookie; an empty path reads as "/". The __Secure- and __Host- prefixes are honoured from the name, Secure for both and path "/" with no domain for __Host-, since the browser rejects a deletion without them. */
func DeleteCookie(response httpcontract.Response, name string, path string) {
    if "" == name {
        exception.Panic(
            exception.NewError("the cookie name is empty and can not be deleted", nil, nil),
        )
    }

    if "" == path {
        path = "/"
    }

    isSecurePrefixed := strings.HasPrefix(name, "__Secure-")
    isHostPrefixed := strings.HasPrefix(name, "__Host-")

    if true == isHostPrefixed {
        path = "/"
    }

    cookie := &nethttp.Cookie{
        Name:     name,
        Value:    "",
        Path:     path,
        MaxAge:   -1,
        HttpOnly: true,
        Secure:   isSecurePrefixed || isHostPrefixed,
    }

    SetCookie(response, cookie)
}
