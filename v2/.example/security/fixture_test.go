package security

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
)

/* The shared test material of the package lives here, and only here: this is the one test file the layout rule exempts from having a source of its own. */

/* requestAccepting builds a request carrying the Accept header the caller names, and no header at all for the empty string. The runtime and the request context are left nil because nothing on the paths under test reads them — the entry point and the access denied handler both reach the underlying http request. */
func requestAccepting(t *testing.T, acceptHeader string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    if "" != acceptHeader {
        httpRequest.Header.Set("Accept", acceptHeader)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, nil)
}

/* requestAcceptingLines is the same, for a client that sent the Accept field as SEVERAL header lines rather than as one comma-joined list. Both spellings are legal and mean the same thing, which is the whole point of the assertions that use it. */
func requestAcceptingLines(t *testing.T, acceptLineList ...string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    for _, acceptLine := range acceptLineList {
        httpRequest.Header.Add("Accept", acceptLine)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, nil)
}
