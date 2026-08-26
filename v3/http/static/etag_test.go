package static

import (
    "io/fs"
    "regexp"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/version"
)

/* the header is a comma-separated list and a proxy may weaken a strong tag; an exact string comparison silently re-sent the whole body for both shapes */
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

/* GenerateEtag is what makes a conditional request answerable at all, and its nil branch had no test: a nil FileInfo has to produce the empty string rather than an entity tag built from a dereference, because the caller reaches here on the path where a stat failed and a panic there runs outside anything that could answer the request. */

func TestGenerateEtag_ANilFileInfoProducesNoTag(t *testing.T) {
    if "" != GenerateEtag(nil, false) {
        t.Fatalf("expected no tag for a nil file info, got: %q", GenerateEtag(nil, false))
    }

    if "" != GenerateEtag(nil, true) {
        t.Fatalf("expected no tag for a nil file info in weak form, got: %q", GenerateEtag(nil, true))
    }
}

/* the weak form carries the W/ prefix and the strong one does not. The distinction is what a proxy is allowed to rewrite, so the two spellings of one file must differ by exactly that prefix — a strong tag emitted where a weak one was configured tells caches the bytes are byte-identical when the application only promised semantic equivalence. */

func TestGenerateEtag_TheWeakFormDiffersOnlyByItsPrefix(t *testing.T) {
    info := &staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 0)}

    strong := GenerateEtag(info, false)
    weak := GenerateEtag(info, true)

    if false == regexp.MustCompile(`^"[0-9a-f]{16}"$`).MatchString(strong) {
        t.Fatalf("expected a quoted sixteen-hex digest as the strong tag, got: %s", strong)
    }

    if "W/"+strong != weak {
        t.Fatalf("expected the weak form to be the strong one behind a W/ prefix: %s vs %s", weak, strong)
    }
}

/* the tag is a digest, and that is a disclosure property, not a rendering choice: spelled out, the tag told every anonymous client the file's modification instant to the nanosecond, and the embedded branch told them the binary's build version, on every asset */
func TestGenerateEtag_DisclosesNeitherTheTimestampNorTheBuildVersion(t *testing.T) {
    dated := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 0)}, false)
    if true == strings.Contains(dated, "1754049600") {
        t.Fatalf("expected the modification instant not to be readable off the tag, got %q", dated)
    }

    undated := GenerateEtag(&staticEtagFileInfo{size: 1024}, false)
    if "" != version.BuildVersion() && true == strings.Contains(undated, version.BuildVersion()) {
        t.Fatalf("expected the build version not to be readable off the tag, got %q", undated)
    }
}

/* the tag changes when either half of it changes — the size or the modification time. A tag built from one of them alone would answer 304 for a deploy that rewrote a file to the same length, and the client would keep the previous bytes until the file changed size. */

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

/* two rewrites within the same second that keep the same length must still produce different tags:
at whole-second resolution they did not, so a deploy that swapped a bundle for one of the same size
revalidated 304 and stayed served stale until its length or its second changed */
func TestGenerateEtag_ChangesWithinTheSameSecond(t *testing.T) {
    earlier := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 100000000)}, false)
    later := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 900000000)}, false)

    if earlier == later {
        t.Fatalf("expected a sub-second modification-time change to produce a different tag, got %q for both", earlier)
    }
}

/* a filesystem that reports no modification time — every embedded one — used to make the tag degenerate into size alone, identical across rebuilds, so a redeployed asset that kept its length revalidated 304 and stayed served stale. The build version stands in for the timestamp there, inside the digest: the undated derivation must differ from every dated one of the same size, and still tell two sizes apart. */
func TestGenerateEtag_AZeroModificationTimeDerivesFromTheBuildVersionInsteadOfTheTimestamp(t *testing.T) {
    zeroTimed := GenerateEtag(&staticEtagFileInfo{size: 1024}, false)

    dated := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 0)}, false)
    if zeroTimed == dated {
        t.Fatalf("expected the undated derivation to differ from the dated one, got %q for both", zeroTimed)
    }

    zeroTimedResized := GenerateEtag(&staticEtagFileInfo{size: 2048}, false)
    if zeroTimed == zeroTimedResized {
        t.Fatalf("expected two undated sizes to produce different tags, got %q for both", zeroTimed)
    }

    zeroTimedAgain := GenerateEtag(&staticEtagFileInfo{size: 1024}, false)
    if zeroTimed != zeroTimedAgain {
        t.Fatalf("expected the undated tag to be stable within one build, got %q and %q", zeroTimed, zeroTimedAgain)
    }
}

/* the weak form of an undated file's tag differs from the strong one only by its prefix, the way it does for a dated one */
func TestGenerateEtag_AZeroModificationTimeKeepsTheWeakFormsPrefix(t *testing.T) {
    strong := GenerateEtag(&staticEtagFileInfo{size: 1024}, false)
    weak := GenerateEtag(&staticEtagFileInfo{size: 1024}, true)

    if "W/"+strong != weak {
        t.Fatalf("expected the weak form to be the strong one behind W/, got %q and %q", weak, strong)
    }
}

/* the field is a list, and a proxy that rewrites it leaves an empty member behind, so ", \"abc\"" is a shape a real cache sends. Skipping the empty member is what keeps it from being compared against the tag; the control below shows that a list of nothing but empty members still matches nothing, so the skip does not turn an empty header into a match. */
func TestEtagMatchesIfNoneMatch_AnEmptyMemberIsSkippedRatherThanCompared(t *testing.T) {
    if false == EtagMatchesIfNoneMatch(", \"abc\"", "\"abc\"") {
        t.Fatalf("expected the tag to be found past an empty member of the list")
    }

    if true == EtagMatchesIfNoneMatch(" , , ", "\"abc\"") {
        t.Fatalf("expected a list carrying nothing but empty members to match no tag")
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
