package static

import (
    "errors"
    "io/fs"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

/* a server that does not cache takes no max age, as Config refuses one beside CacheDisabled */
func newTestConfig(
    mode Mode,
    publicDir string,
    indexFile string,
    stripPrefix string,
    enableCache bool,
    cacheMaxAge int,
    weakEtag bool,
) Config {
    config := Config{
        Mode:          mode,
        PublicDir:     publicDir,
        IndexFile:     indexFile,
        StripPrefix:   stripPrefix,
        CacheDisabled: false == enableCache,
        WeakEtag:      weakEtag,
    }

    if true == enableCache {
        config.CacheMaxAge = bound.Of(cacheMaxAge)
    }

    return config
}

func newTestSource(config Config, root string, fileSystem fs.FS) Config {
    config.Root = root
    config.FileSystem = fileSystem

    return config
}

func assertRefusedNamingField(t *testing.T, config Config, expectedMessage string, field string) {
    t.Helper()

    defer func() {
        recovered := recover()

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected a refusal %q, got %v", expectedMessage, recovered)
        }

        var refusal *exception.Error
        if false == errors.As(recoveredErr, &refusal) {
            t.Fatalf("expected an exception error, got %T", recoveredErr)
        }

        if expectedMessage != refusal.Message() {
            t.Fatalf("expected the message %q, got %q", expectedMessage, refusal.Message())
        }

        if field != refusal.Context()["field"] {
            t.Fatalf("expected the refusal to name %q, got %v", field, refusal.Context()["field"])
        }
    }()

    NewFileServer(config)
}
