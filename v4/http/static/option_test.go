package static

import (
    "testing"
    "testing/fstest"
)

func newTestEmbeddedServer(config Config) *FileServer {
    config.Mode = ModeEmbedded
    config.FileSystem = fstest.MapFS{}

    return NewFileServer(config)
}

func TestConfig_AnEmptyAllowedDotPrefixListRefusesEveryDotPrefix(t *testing.T) {
    server := newTestEmbeddedServer(Config{AllowedDotPrefixList: []string{}})

    if 0 != len(server.config.allowedDotPrefixList) {
        t.Fatalf("expected an empty list to allow nothing, got: %v", server.config.allowedDotPrefixList)
    }

    if false == hasDotPrefixedPathElement("/.well-known/acme-challenge/token", server.config.allowedDotPrefixList) {
        t.Fatalf("expected an empty allowance to refuse even the well-known directory")
    }
}

func TestConfig_ANilExcludedPathListExcludesNothing(t *testing.T) {
    server := newTestEmbeddedServer(Config{})

    if 0 != len(server.config.excludedPathList) {
        t.Fatalf("expected a nil list to exclude nothing, got: %v", server.config.excludedPathList)
    }

    if true == hasExcludedPathPrefix("/internal/secret.json", server.config.excludedPathList) {
        t.Fatalf("expected a nil exclusion list to exclude nothing")
    }
}

/* an entry without a leading slash is compared as a prefix against a request path that always begins with one, so it could never match and silently excluded nothing — the server normalizes it to the shape that matches */
func TestConfig_AnExcludedPathWithoutItsLeadingSlashIsNormalized(t *testing.T) {
    server := newTestEmbeddedServer(Config{ExcludedPathList: []string{"admin", "/api"}})

    if 2 != len(server.config.excludedPathList) || "/admin" != server.config.excludedPathList[0] || "/api" != server.config.excludedPathList[1] {
        t.Fatalf("expected the missing leading slash normalized and the slashed entry kept, got: %v", server.config.excludedPathList)
    }

    if false == hasExcludedPathPrefix("/admin/panel", server.config.excludedPathList) {
        t.Fatalf("expected the normalized entry to exclude the path it names")
    }
}

func TestConfig_ANilAllowedDotPrefixListAllowsTheWellKnownDirectoryAndNothingElse(t *testing.T) {
    server := newTestEmbeddedServer(Config{})

    if 1 != len(server.config.allowedDotPrefixList) || DefaultAllowedDotPrefix != server.config.allowedDotPrefixList[0] {
        t.Fatalf("unexpected default allowance: %v", server.config.allowedDotPrefixList)
    }

    if true == hasDotPrefixedPathElement("/.well-known/acme-challenge/token", server.config.allowedDotPrefixList) {
        t.Fatalf("expected the well-known directory to be retrievable by default")
    }

    if false == hasDotPrefixedPathElement("/.env", server.config.allowedDotPrefixList) {
        t.Fatalf("expected a dot-prefixed file to stay refused by default")
    }

    if false == hasDotPrefixedPathElement("/.well-known/.env", server.config.allowedDotPrefixList) {
        t.Fatalf("expected the allowance not to reach past the first path element")
    }
}
