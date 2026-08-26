package http

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
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

/* DeleteCookie writes the expiring counterpart of the named cookie; an empty path reads as "/". The __Host- and __Secure- prefixes are contracts the browser enforces on EVERY Set-Cookie, the deleting one included: a __Secure- cookie must be marked Secure, and a __Host- cookie must be Secure with path "/" and no domain — a deletion written without them is rejected by the browser in silence, so the cookie this function promised to delete simply stayed. The prefixed shapes are therefore taken from the name, path included: for a __Host- name the one path the browser accepts wins over the one the caller passed, because the caller's intent is the deletion, and the passed path could only make it not happen. */
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
