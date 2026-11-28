package static

import (
    "io/fs"
    "regexp"
    "strings"
    "sync"
    "testing"
    "testing/fstest"
    "time"

    "github.com/precision-soft/melody/v4/version"
)

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

func TestGenerateEtag_ANilFileInfoProducesNoTag(t *testing.T) {
    if "" != GenerateEtag(nil, false) {
        t.Fatalf("expected no tag for a nil file info, got: %q", GenerateEtag(nil, false))
    }

    if "" != GenerateEtag(nil, true) {
        t.Fatalf("expected no tag for a nil file info in weak form, got: %q", GenerateEtag(nil, true))
    }
}

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

func TestGenerateEtag_ChangesWithinTheSameSecond(t *testing.T) {
    earlier := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 100000000)}, false)
    later := GenerateEtag(&staticEtagFileInfo{size: 1024, modTime: time.Unix(1754049600, 900000000)}, false)

    if earlier == later {
        t.Fatalf("expected a sub-second modification-time change to produce a different tag, got %q for both", earlier)
    }
}

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

func TestGenerateEtag_AZeroModificationTimeKeepsTheWeakFormsPrefix(t *testing.T) {
    strong := GenerateEtag(&staticEtagFileInfo{size: 1024}, false)
    weak := GenerateEtag(&staticEtagFileInfo{size: 1024}, true)

    if "W/"+strong != weak {
        t.Fatalf("expected the weak form to be the strong one behind W/, got %q and %q", weak, strong)
    }
}

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

func TestFileServer_AnEmbeddedAssetTagIsWeakUnderTheWeakOption(t *testing.T) {
    tagOf := func(weak bool) string {
        server := &FileServer{
            config:      &fileServerConfig{weakEtag: weak},
            fileSystem:  fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte("body{color:red}")}},
            contentTags: &sync.Map{},
        }

        info, statErr := fs.Stat(server.fileSystem, "app.css")
        if nil != statErr {
            t.Fatalf("stat: %v", statErr)
        }

        return server.entityTag("app.css", info)
    }

    strong := tagOf(false)
    weak := tagOf(true)

    if true == strings.HasPrefix(strong, "W/") || "W/"+strong != weak {
        t.Fatalf("expected the weak tag to be the strong one behind W/, got %s and %s", strong, weak)
    }
}

func TestFileServer_TwoEmbeddedAssetsOfTheSameSizeKeepTheirOwnTags(t *testing.T) {
    server := &FileServer{
        config: &fileServerConfig{},
        fileSystem: fstest.MapFS{
            "red.css": &fstest.MapFile{Data: []byte("body{color:red}")},
            "tan.css": &fstest.MapFile{Data: []byte("body{color:tan}")},
        },
        contentTags: &sync.Map{},
    }

    tagOf := func(name string) string {
        info, statErr := fs.Stat(server.fileSystem, name)
        if nil != statErr {
            t.Fatalf("stat: %v", statErr)
        }

        return server.entityTag(name, info)
    }

    red := tagOf("red.css")
    tan := tagOf("tan.css")
    redAgain := tagOf("red.css")

    if red == tan {
        t.Fatalf("expected two assets of the same size and different bytes to get two tags, both were %s", red)
    }

    if red != redAgain {
        t.Fatalf("expected the first asset to keep its tag, got %s and %s", red, redAgain)
    }
}

func TestFileServer_AnEmbeddedAssetChangedAtTheSameSizeGetsANewTag(t *testing.T) {
    tagOf := func(content string) string {
        server := &FileServer{
            config:      &fileServerConfig{},
            fileSystem:  fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte(content)}},
            contentTags: &sync.Map{},
        }

        info, statErr := fs.Stat(server.fileSystem, "app.css")
        if nil != statErr {
            t.Fatalf("stat: %v", statErr)
        }

        if false == info.ModTime().IsZero() {
            t.Fatalf("expected the embedded-shaped file to carry no modification time")
        }

        return server.entityTag("app.css", info)
    }

    before := tagOf("body{color:red}")
    after := tagOf("body{color:tan}")
    same := tagOf("body{color:red}")

    if before == after {
        t.Fatalf("expected a changed asset of the same size to get a new tag, both were %s", before)
    }

    if before != same {
        t.Fatalf("expected the same bytes to keep the same tag, got %s and %s", before, same)
    }
}
