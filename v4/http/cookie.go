package http

import (
    nethttp "net/http"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* SetCookie adds the cookie to the response and leaves Cache-Control alone: whether a response addresses one client is the caller's decision. The session path marks the session cookie's response private itself. */
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

    /* nil headers are a state the contract permits, so the map is created here */
    if nil == response.Headers() {
        response.SetHeaders(make(nethttp.Header))
    }

    response.Headers().Add("Set-Cookie", serialized)
}

/* DeleteCookie writes the expiring counterpart of the cookie, read by its Name, Path and Domain, as SetCookie writes the cookie itself: an empty Path reads as "/", and a cookie set with a Domain is matched by the browser on that domain alone, so the deletion must name it. The __Secure- and __Host- prefixes are honoured from the name, Secure for both and path "/" with no domain for __Host-, since the browser rejects a deletion without them. A nil cookie and an empty name are refused. Every other field is ignored: the deletion carries no value, MaxAge -1 and HttpOnly. */
func DeleteCookie(response httpcontract.Response, cookie *nethttp.Cookie) {
    if nil == cookie {
        exception.Panic(
            exception.NewError("the cookie is nil and can not be deleted", nil, nil),
        )
    }

    name := cookie.Name
    path := cookie.Path
    domain := cookie.Domain

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
        domain = ""
    }

    expiring := &nethttp.Cookie{
        Name:     name,
        Value:    "",
        Path:     path,
        Domain:   domain,
        MaxAge:   -1,
        HttpOnly: true,
        Secure:   isSecurePrefixed || isHostPrefixed,
    }

    SetCookie(response, expiring)
}
