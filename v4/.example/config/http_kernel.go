package config

import (
    "github.com/precision-soft/melody/v4/.example/handler"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

/* HttpKernelConfig is the example's http kernel: its own not found page and the forwarded scheme believed from the private ranges; MELODY_HTTP_SESSION_COOKIE_SECURE=always marks the session cookie Secure behind a proxy outside them. */
func HttpKernelConfig() melodyhttp.KernelConfig {
    return melodyhttp.KernelConfig{
        NotFoundHandler:        handler.NotFoundHandler(),
        ForwardedHeadersPolicy: exampleForwardedHeadersPolicy(),
    }
}
