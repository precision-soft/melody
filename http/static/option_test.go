package static

import (
    "testing"
)

func newTestFileServerConfig() *FileServerConfig {
    return NewFileServerConfig(ModeFilesystem, "public", "index.html", "", false, 0, false)
}

/* @info the two list setters decide what the file server refuses without looking at the disk, and each keeps its own copy. A setter that aliased the caller's slice would let a configuration value mutated after the boot rewrite the refusals of a running file server — the ones that keep .env and .git out of the public directory. */

func TestFileServerConfig_SetAllowedDotPrefixListKeepsItsOwnCopy(t *testing.T) {
    config := newTestFileServerConfig()

    supplied := []string{".well-known"}
    config.SetAllowedDotPrefixList(supplied)

    supplied[0] = ".env"

    if 1 != len(config.allowedDotPrefixList) || ".well-known" != config.allowedDotPrefixList[0] {
        t.Fatalf("expected the configuration to keep its own copy, got: %v", config.allowedDotPrefixList)
    }

    if false == hasDotPrefixedPathElement("/.env", config.allowedDotPrefixList) {
        t.Fatalf("a mutation of the caller's slice opened the very path the refusal exists for")
    }
}

func TestFileServerConfig_SetExcludedPathListKeepsItsOwnCopy(t *testing.T) {
    config := newTestFileServerConfig()

    supplied := []string{"/internal"}
    config.SetExcludedPathList(supplied)

    supplied[0] = "/"

    if 1 != len(config.excludedPathList) || "/internal" != config.excludedPathList[0] {
        t.Fatalf("expected the configuration to keep its own copy, got: %v", config.excludedPathList)
    }

    if true == hasExcludedPathPrefix("/articles", config.excludedPathList) {
        t.Fatalf("a mutation of the caller's slice turned one excluded prefix into every path")
    }
}

/* @info an explicit nil is read as "allow no dot prefix at all" rather than left as whatever was there before: a deployment that clears the list means to refuse every dot-prefixed path, and keeping the previous entry would leave one open that the operator believes closed. */

func TestFileServerConfig_SetAllowedDotPrefixListOfNilRefusesEveryDotPrefix(t *testing.T) {
    config := newTestFileServerConfig()

    config.SetAllowedDotPrefixList(nil)

    if 0 != len(config.allowedDotPrefixList) {
        t.Fatalf("expected a nil list to allow nothing, got: %v", config.allowedDotPrefixList)
    }

    if false == hasDotPrefixedPathElement("/.well-known/acme-challenge/token", config.allowedDotPrefixList) {
        t.Fatalf("expected a cleared allowance to refuse even the well-known directory")
    }
}

func TestFileServerConfig_SetExcludedPathListOfNilExcludesNothing(t *testing.T) {
    config := newTestFileServerConfig()

    config.SetExcludedPathList([]string{"/internal"})
    config.SetExcludedPathList(nil)

    if 0 != len(config.excludedPathList) {
        t.Fatalf("expected a nil list to exclude nothing, got: %v", config.excludedPathList)
    }

    if true == hasExcludedPathPrefix("/internal/secret.json", config.excludedPathList) {
        t.Fatalf("expected a cleared exclusion list to exclude nothing")
    }
}

/* @info the shipped default allows the well-known directory and nothing else — an ACME http-01 challenge and security.txt stay retrievable while .env, .git and .htpasswd do not — and the allowance never reaches past the first path element. */

func TestNewFileServerConfig_AllowsTheWellKnownDirectoryAndNothingElse(t *testing.T) {
    config := newTestFileServerConfig()

    if 1 != len(config.allowedDotPrefixList) || DefaultAllowedDotPrefix != config.allowedDotPrefixList[0] {
        t.Fatalf("unexpected default allowance: %v", config.allowedDotPrefixList)
    }

    if true == hasDotPrefixedPathElement("/.well-known/acme-challenge/token", config.allowedDotPrefixList) {
        t.Fatalf("expected the well-known directory to be retrievable by default")
    }

    if false == hasDotPrefixedPathElement("/.env", config.allowedDotPrefixList) {
        t.Fatalf("expected a dot-prefixed file to stay refused by default")
    }

    if false == hasDotPrefixedPathElement("/.well-known/.env", config.allowedDotPrefixList) {
        t.Fatalf("expected the allowance not to reach past the first path element")
    }

    if 0 != len(config.excludedPathList) {
        t.Fatalf("expected the default excluded list to exclude nothing, got: %v", config.excludedPathList)
    }
}
