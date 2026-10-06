package config

import (
    nethttp "net/http"
    "strings"

    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* sessionCookieSecureAlways is the value of APP_SESSION_COOKIE_SECURE that marks the session cookie Secure on every response, for a deployment behind a proxy that terminates tls from outside the ranges the forwarded scheme is believed from */
const sessionCookieSecureAlways = "always"

/* sessionCookiePolicySetter is the kernel door the policy is installed through */
type sessionCookiePolicySetter interface {
    SetSessionCookiePolicy(policy melodyhttpcontract.SessionCookiePolicy)
}

/* applySessionCookieSecure reads APP_SESSION_COOKIE_SECURE: "always" installs the kernel's default policy with the cookie marked Secure whatever the scheme, the path, the domain and SameSite restated as the kernel's own defaults since the kernel answers no getter; an empty value or "from-scheme" leaves the kernel's default, which derives Secure from the scheme believed from the forwarded-headers list; any other value refuses the boot naming the key, since a typo would leave the cookie unmarked in silence. */
func applySessionCookieSecure(setter sessionCookiePolicySetter, value string) {
    switch strings.TrimSpace(value) {
    case "", "from-scheme":
        return
    case sessionCookieSecureAlways:
        setter.SetSessionCookiePolicy(melodyhttpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
            Secure:   melodyhttpcontract.SessionCookieSecureAlways,
        })
    default:
        melodyexception.Panic(melodyexception.NewError(
            "the session cookie secure switch reads always or from-scheme",
            map[string]any{"key": "APP_SESSION_COOKIE_SECURE", "value": value},
            nil,
        ))
    }
}
