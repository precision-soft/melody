package httpclient

import (
    "testing"
)

/* v3 carries no explicit-cap flag: the default cap binds the streaming path exactly as it binds the buffered one, so the option set holds only the value. The default and an override are pinned here. */
func TestRequestOptions_MaxResponseBodyBytesDefaultAndOverride(t *testing.T) {
    options := NewRequestOptions()

    if 10*1024*1024 != options.MaxResponseBodyBytes() {
        t.Fatalf("unexpected default cap: %d", options.MaxResponseBodyBytes())
    }

    WithMaxResponseBodyBytes(10)(options)

    if 10 != options.MaxResponseBodyBytes() {
        t.Fatalf("unexpected cap: %d", options.MaxResponseBodyBytes())
    }
}

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

/* the plural setter is a canonicalizing door of its own: ranging over the caller's raw map would store the raw spellings and leave the singular setter guarding a map that is already ambiguous. */
func TestRequestOptions_SetHeadersCanonicalizesAtTheDoor(t *testing.T) {
    options := NewRequestOptions()

    options.SetHeaders(map[string]string{"x-api-key": "secret"})

    headers := options.Headers()
    if 1 != len(headers) || "secret" != headers["X-Api-Key"] {
        t.Fatalf("expected the plural setter to store the canonical spelling, got %#v", headers)
    }
}

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

/* the getters hand out copies: a write through the returned map used to bypass the canonicalization SetHeader enforces, and the request-time winner between the planted spelling and the canonical one was chosen by map iteration — in what is often a credential header. */
func TestRequestOptions_HeadersHandsOutACopy(t *testing.T) {
    options := NewRequestOptions()
    options.SetHeader("X-Api-Key", "canonical")

    options.Headers()["x-api-key"] = "planted"

    if 1 != len(options.Headers()) {
        t.Fatalf("expected the planted spelling to land on the copy only, got %v", options.Headers())
    }

    if "canonical" != options.Headers()["X-Api-Key"] {
        t.Fatalf("expected the canonical entry untouched, got %q", options.Headers()["X-Api-Key"])
    }
}

func TestRequestOptions_QueryHandsOutACopy(t *testing.T) {
    options := NewRequestOptions()
    options.SetQuery("page", "1")

    options.Query()["page"] = "2"

    if "1" != options.Query()["page"] {
        t.Fatalf("expected the stored parameter untouched, got %q", options.Query()["page"])
    }
}
