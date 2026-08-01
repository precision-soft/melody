package httpclient

import (
    "testing"
)

/* @info The streaming path applies a cap the caller named and leaves an unnamed one to the buffered path, which holds the whole body in memory; the two are told apart by whether the option was ever applied, not by the value it carries. */
func TestRequestOptions_ExplicitMaxResponseBodyBytesIsDistinguishedFromTheDefault(t *testing.T) {
    options := NewRequestOptions()

    if true == options.hasExplicitMaxResponseBodyBytes() {
        t.Fatalf("a fresh option set carries the default, not a caller's cap")
    }
    if 10*1024*1024 != options.MaxResponseBodyBytes() {
        t.Fatalf("unexpected default cap: %d", options.MaxResponseBodyBytes())
    }

    WithMaxResponseBodyBytes(10)(options)

    if false == options.hasExplicitMaxResponseBodyBytes() {
        t.Fatalf("expected an applied option to be recorded as the caller's own")
    }
    if 10 != options.MaxResponseBodyBytes() {
        t.Fatalf("unexpected cap: %d", options.MaxResponseBodyBytes())
    }
}

/* @info A caller who names the same value the default carries still named it, and the streaming path honours it. */
func TestRequestOptions_ExplicitCapEqualToTheDefaultIsStillExplicit(t *testing.T) {
    options := NewRequestOptions()

    WithMaxResponseBodyBytes(10 * 1024 * 1024)(options)

    if false == options.hasExplicitMaxResponseBodyBytes() {
        t.Fatalf("expected a cap equal to the default to count as the caller's own")
    }
}

/* @info the four plural options had never been executed by anything, and they are the ones an application reaches for when the headers come from configuration rather than from literals. They MERGE rather than replace — a header set singly survives a later plural call that does not name it — and they copy entry by entry into the option set's own map, so the caller's map is not the one the request reads. A nil map is a no-op rather than a panic, which is the shape a configuration that resolved to nothing takes. */
func TestRequestOptions_SetHeadersMergesAndCopies(t *testing.T) {
    options := NewRequestOptions()

    options.SetHeader("X-Single", "single")

    callerHeaders := map[string]string{"X-Plural": "plural"}
    options.SetHeaders(callerHeaders)

    if "single" != options.Headers()["X-Single"] {
        t.Fatalf("expected the singly set header to survive the plural call, got %q", options.Headers()["X-Single"])
    }

    if "plural" != options.Headers()["X-Plural"] {
        t.Fatalf("expected the plural header to be recorded, got %q", options.Headers()["X-Plural"])
    }

    callerHeaders["X-Plural"] = "mutated"
    callerHeaders["X-Late"] = "late"

    if "plural" != options.Headers()["X-Plural"] {
        t.Fatalf("expected the option set to hold its own copy of the caller's entry, got %q", options.Headers()["X-Plural"])
    }

    if _, leaked := options.Headers()["X-Late"]; true == leaked {
        t.Fatalf("expected an entry added to the caller's map afterwards not to reach the option set")
    }

    options.SetHeaders(nil)

    if 2 != len(options.Headers()) {
        t.Fatalf("expected a nil map to change nothing, got %v", options.Headers())
    }
}

/* @info the query counterpart carries the same contract, and it has to be pinned separately: the two are written as a pair and a copy dropped from one of them would leak only through whichever of the two the application happens to use. */
func TestRequestOptions_SetQueryParamsMergesAndCopies(t *testing.T) {
    options := NewRequestOptions()

    options.SetQuery("single", "one")

    callerParameters := map[string]string{"plural": "two"}
    options.SetQueryParams(callerParameters)

    if "one" != options.Query()["single"] {
        t.Fatalf("expected the singly set parameter to survive the plural call, got %q", options.Query()["single"])
    }

    if "two" != options.Query()["plural"] {
        t.Fatalf("expected the plural parameter to be recorded, got %q", options.Query()["plural"])
    }

    callerParameters["plural"] = "mutated"

    if "two" != options.Query()["plural"] {
        t.Fatalf("expected the option set to hold its own copy, got %q", options.Query()["plural"])
    }

    options.SetQueryParams(nil)

    if 2 != len(options.Query()) {
        t.Fatalf("expected a nil map to change nothing, got %v", options.Query())
    }
}

/* @info WithHeaders and WithQueryParams are the option-shaped forms, and they differ from their singular siblings in a way nothing pinned: WithHeader captures two immutable strings at the moment it is built, while WithHeaders captures the caller's MAP and reads it only when the option is applied — which happens inside the request call. This test records the behaviour as it stands today, not as it ought to be: a mutation made between building the option and issuing the request DOES change what is sent, and a mutation made concurrently with a request in flight is a race on a map the caller believes it handed over. Carried to the backlog as a decision to take, alongside the response header map. */
func TestRequestOptions_WithHeadersAndWithQueryParamsReadTheCallersMapWhenApplied(t *testing.T) {
    callerHeaders := map[string]string{"X-Plural": "at-build-time"}
    callerParameters := map[string]string{"plural": "at-build-time"}

    headerOption := WithHeaders(callerHeaders)
    queryOption := WithQueryParams(callerParameters)

    singularOption := WithHeader("X-Single", callerHeaders["X-Plural"])

    callerHeaders["X-Plural"] = "at-apply-time"
    callerParameters["plural"] = "at-apply-time"

    options := NewRequestOptions()
    headerOption(options)
    queryOption(options)
    singularOption(options)

    if "at-apply-time" != options.Headers()["X-Plural"] {
        t.Fatalf("expected the plural option to read the caller's map when applied, got %q", options.Headers()["X-Plural"])
    }

    if "at-apply-time" != options.Query()["plural"] {
        t.Fatalf("expected the plural query option to read the caller's map when applied, got %q", options.Query()["plural"])
    }

    if "at-build-time" != options.Headers()["X-Single"] {
        t.Fatalf("expected the singular option to have captured its value when it was built, got %q", options.Headers()["X-Single"])
    }
}
