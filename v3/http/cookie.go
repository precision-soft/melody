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

/* DeleteCookie writes the expiring counterpart of the named host-only cookie; an empty path reads as "/". A cookie set with a Domain is matched by the browser on its domain and is not reached: DeleteCookieWithDomain deletes it. The __Secure- and __Host- prefixes are honoured from the name, Secure for both and path "/" with no domain for __Host-, since the browser rejects a deletion without them. */
func DeleteCookie(response httpcontract.Response, name string, path string) {
    deleteCookie(response, name, path, "")
}

/* DeleteCookieWithDomain writes the expiring counterpart of the named cookie set with domain, as DeleteCookie does for a host-only one; a __Host- name drops the domain, which its prefix forbids. */
func DeleteCookieWithDomain(response httpcontract.Response, name string, path string, domain string) {
    deleteCookie(response, name, path, domain)
}

func deleteCookie(response httpcontract.Response, name string, path string, domain string) {
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

    cookie := &nethttp.Cookie{
        Name:     name,
        Value:    "",
        Path:     path,
        Domain:   domain,
        MaxAge:   -1,
        HttpOnly: true,
        Secure:   isSecurePrefixed || isHostPrefixed,
    }

    SetCookie(response, cookie)
}
