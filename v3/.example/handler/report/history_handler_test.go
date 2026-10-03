package report

import (
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
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

/* the shape of a query parameter is chosen by the client, so a repeated key at this public door is answered rather than refused: the limit is the first value, as for a single key. */
func TestHistoryLimitOf_TakesTheFirstValueOfARepeatedKey(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=2&limit=3"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if 2 != limit {
        t.Fatalf("expected the first value of the repeated key, got %d", limit)
    }
}

/* a failure of the archive is rendered through the presenter — the envelope every sibling read door answers
   a repository failure in — as a Response, never returned for the kernel's exception listener to render in a
   second shape: one client reads one envelope for one class of failure. A container without the report
   service is the cheapest such failure. */
func TestApiHistoryHandler_RendersAnArchiveFailureThroughThePresenter(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, "/reports/api/history/", nil),
        map[string]string{},
        runtimeInstance,
        melodyhttp.NewRequestContext("history-door-test", time.Unix(0, 0).UTC()),
    )

    response, handlerErr := ApiHistoryHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("the archive's failure was returned for the kernel to render, not answered through the presenter: %v", handlerErr)
    }

    if nil == response || nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected the presenter's 500, got %v", response)
    }

    body, _ := io.ReadAll(response.BodyReader())
    if false == strings.Contains(string(body), `"success":false`) || false == strings.Contains(string(body), "the reading archive is unavailable") {
        t.Fatalf("expected the application's envelope, got %s", body)
    }
}
