package report

import (
    "net/http/httptest"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    nethttp "net/http"
    "testing"
    "time"
)

func historyRequest(t *testing.T, target string) melodyhttpcontract.Request {
    t.Helper()

    return melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, target, nil),
        map[string]string{},
        nil,
        melodyhttp.NewRequestContext("history-door-test", time.Unix(0, 0).UTC()),
    )
}
