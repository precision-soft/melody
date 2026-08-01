package static

import (
    "io/fs"
    "testing"
    "time"
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

/* @info GenerateEtag is what makes a conditional request answerable at all, and its nil branch had no test: a nil FileInfo has to produce the empty string rather than an entity tag built from a dereference, because the caller reaches here on the path where a stat failed and a panic there runs outside anything that could answer the request. */

func TestGenerateEtag_ANilFileInfoProducesNoTag(t *testing.T) {
    if "" != GenerateEtag(nil, false) {
        t.Fatalf("expected no tag for a nil file info, got: %q", GenerateEtag(nil, false))
    }

    if "" != GenerateEtag(nil, true) {
        t.Fatalf("expected no tag for a nil file info in weak form, got: %q", GenerateEtag(nil, true))
    }
}

/* @info the weak form carries the W/ prefix and the strong one does not. The distinction is what a proxy is allowed to rewrite, so the two spellings of one file must differ by exactly that prefix — a strong tag emitted where a weak one was configured tells caches the bytes are byte-identical when the application only promised semantic equivalence. */

func TestGenerateEtag_TheWeakFormDiffersOnlyByItsPrefix(t *testing.T) {
    info := &staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 0)}

    strong := GenerateEtag(info, false)
    weak := GenerateEtag(info, true)

    if `"1024-1754049600"` != strong {
        t.Fatalf("unexpected strong tag: %s", strong)
    }

    if `W/"1024-1754049600"` != weak {
        t.Fatalf("unexpected weak tag: %s", weak)
    }

    if "W/"+strong != weak {
        t.Fatalf("expected the weak form to be the strong one behind a W/ prefix: %s vs %s", weak, strong)
    }
}

/* @info the tag changes when either half of it changes — the size or the modification time. A tag built from one of them alone would answer 304 for a deploy that rewrote a file to the same length, and the client would keep the previous bytes until the file changed size. */

func TestGenerateEtag_ChangesWithEitherTheSizeOrTheModificationTime(t *testing.T) {
    base := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 0)}, false)
    resized := GenerateEtag(&staticEtagFileInfo{size: 2048, modTime: time.Unix(1754049600, 0)}, false)
    retouched := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049601, 0)}, false)

    if base == resized {
        t.Fatalf("expected a different size to produce a different tag")
    }

    if base == retouched {
        t.Fatalf("expected a different modification time to produce a different tag")
    }
}

type staticEtagFileInfo struct {
    size    int64
    modTime time.Time
}

func (instance *staticEtagFileInfo) Name() string { return "app.css" }

func (instance *staticEtagFileInfo) Size() int64 { return instance.size }

func (instance *staticEtagFileInfo) Mode() fs.FileMode { return 0o644 }

func (instance *staticEtagFileInfo) ModTime() time.Time { return instance.modTime }

func (instance *staticEtagFileInfo) IsDir() bool { return false }

func (instance *staticEtagFileInfo) Sys() any { return nil }
