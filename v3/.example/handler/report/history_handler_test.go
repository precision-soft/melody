package report

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* the limit door reads nothing off the runtime, so the request is built without one: what it needs is the query bag, which NewRequest fills from the url at construction. */
func historyRequest(t *testing.T, target string) melodyhttpcontract.Request {
    t.Helper()

    return melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, target, nil),
        map[string]string{},
        nil,
        melodyhttp.NewRequestContext("history-door-test", time.Unix(0, 0).UTC()),
    )
}

func TestHistoryLimitOf_AnswersTheDefaultWhenTheCallerAskedForNoLimit(t *testing.T) {
    for _, target := range []string{
        "/reports/api/history/",
        "/reports/api/history/?limit=",
        "/reports/api/history/?other=5",
    } {
        limit, limitErr := historyLimitOf(historyRequest(t, target))
        if nil != limitErr {
            t.Fatalf("%s: %v", target, limitErr)
        }

        if historyDefaultLimit != limit {
            t.Fatalf("%s: expected the default %d, got %d", target, historyDefaultLimit, limit)
        }
    }
}

func TestHistoryLimitOf_ReadsTheCallersLimit(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=3"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if 3 != limit {
        t.Fatalf("expected 3, got %d", limit)
    }
}

/* every way of asking for a limit this door cannot serve is the same mistake and gets the same words, so the refusal is asserted on all three rather than on whichever one came to mind. */
func TestHistoryLimitOf_RefusesALimitItCannotServe(t *testing.T) {
    for _, target := range []string{
        "/reports/api/history/?limit=abc",
        "/reports/api/history/?limit=0",
        "/reports/api/history/?limit=-3",
        "/reports/api/history/?limit=1.5",
    } {
        _, limitErr := historyLimitOf(historyRequest(t, target))
        if nil == limitErr {
            t.Fatalf("%s: expected a refusal", target)
        }

        if errInvalidLimit != limitErr {
            t.Fatalf("%s: expected the door's one refusal, got %v", target, limitErr)
        }
    }
}

/* the ceiling is the door's own: the archive grows for the life of a volume, so a limit taken from the request without a cap is an unauthenticated caller choosing how much of it this process loads at once. */
func TestHistoryLimitOf_CapsWhatACallerCanAskFor(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=9999"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if historyMaximumLimit != limit {
        t.Fatalf("expected the ceiling %d, got %d", historyMaximumLimit, limit)
    }
}

/* the parameter is read with StringAt rather than with the string accessor beside it, because that one PANICS on a repeated key — the shape of a query parameter is chosen by the client, so through a public door that would be an unauthenticated 500. What a repeated key answers here is the first value, and this is the test that says so: it fails by panicking, not by returning the wrong number, which is exactly the failure it guards. */
func TestHistoryLimitOf_TakesTheFirstValueOfARepeatedKeyWithoutPanicking(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=2&limit=3"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if 2 != limit {
        t.Fatalf("expected the first value of the repeated key, got %d", limit)
    }
}
