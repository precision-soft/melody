package report

import (
    "context"
    "io"
    "strings"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    "net/http/httptest"
    "testing"
)

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

func TestHistoryLimitOf_CapsWhatACallerCanAskFor(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=9999"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if historyMaximumLimit != limit {
        t.Fatalf("expected the ceiling %d, got %d", historyMaximumLimit, limit)
    }
}

func TestHistoryLimitOf_TakesTheFirstValueOfARepeatedKeyWithoutPanicking(t *testing.T) {
    limit, limitErr := historyLimitOf(historyRequest(t, "/reports/api/history/?limit=2&limit=3"))
    if nil != limitErr {
        t.Fatalf("limit: %v", limitErr)
    }

    if 2 != limit {
        t.Fatalf("expected the first value of the repeated key, got %d", limit)
    }
}

func TestHistoryFailureUsesThePublicApiEnvelope(t *testing.T) {
    containerInstance := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), containerInstance.NewScope(), containerInstance)
    response, err := ApiHistoryHandler()(runtimeInstance, httptest.NewRecorder(), historyRequest(t, "/reports/api/history/"))
    if nil != err || nil == response { t.Fatalf("missing API error response: response=%v err=%v", response, err) }
    body, err := io.ReadAll(response.BodyReader())
    if nil != err || 500 != response.StatusCode() || false == strings.Contains(string(body), `"success":false`) { t.Fatalf("invalid API error: %s err=%v", body, err) }
}
