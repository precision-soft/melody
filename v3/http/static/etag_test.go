package static

import (
    "testing"
)

/* @info the header is a comma-separated list and a proxy may weaken a strong tag; an exact string comparison silently re-sent the whole body for both shapes */
func TestEtagMatchesIfNoneMatch(t *testing.T) {
    etag := `"1024-1717000000"`

    for _, testCase := range []struct {
        ifNoneMatch string
        expected    bool
    }{
        {etag, true},
        {`"deadbeef", ` + etag, true},
        {etag + ` , "deadbeef"`, true},
        {`W/` + etag, true},
        {` ` + etag, true},
        {`"deadbeef"`, false},
        {"", false},
        {"*", false},
    } {
        if testCase.expected != EtagMatchesIfNoneMatch(testCase.ifNoneMatch, etag) {
            t.Fatalf("if-none-match %q: expected %v", testCase.ifNoneMatch, testCase.expected)
        }
    }

    if true == EtagMatchesIfNoneMatch(etag, "") {
        t.Fatalf("expected no match when the server has no etag")
    }

    if false == EtagMatchesIfNoneMatch(etag, `W/`+etag) {
        t.Fatalf("expected a weak server etag to match a strong client entry")
    }
}
