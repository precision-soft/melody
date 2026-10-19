package cli

import (
    "bytes"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
)

/* the command empties the cache the application reads, once, says what the clear reached, and touches neither
   database: the runtime carries a catalogue and an archive that are not persistent, so any step of the reset
   would have nothing to act on and the clear is the whole of the run */
func TestCacheClearCommandEmptiesTheCacheAndSaysWhatItReached(t *testing.T) {
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(t, persistence.NewCatalogStorage(nil), persistence.NewArchiveStorage(nil))

    cache := melodycontainer.MustFromResolver[melodycachecontract.Cache](runtimeInstance.Container(), melodycache.ServiceCache)
    if setErr := cache.Set("zz-cached", "value", time.Hour); nil != setErr {
        t.Fatalf("planting an entry failed: %v", setErr)
    }

    buffer := &bytes.Buffer{}
    if runErr := NewCacheClearCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, buffer)); nil != runErr {
        t.Fatalf("expected the clear to complete, got %v", runErr)
    }

    if 1 != cacheInstance.clears() {
        t.Errorf("expected one clear, got %d", cacheInstance.clears())
    }

    if _, exists, _ := cache.Get("zz-cached"); true == exists {
        t.Errorf("the planted entry survived the clear")
    }

    if "cache cleared: "+cacheClearedScope(runtimeInstance)+"\n" != buffer.String() {
        t.Errorf("expected the one line naming what the clear reached, got %q", buffer.String())
    }
}

/* a clear that fails takes the exit code, under the name of the command that ran it — the reset's own refusal
   names the reset, so the operator reads which of the two doors stopped */
func TestCacheClearCommandNamesItselfWhenTheClearFails(t *testing.T) {
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(t, persistence.NewCatalogStorage(nil), persistence.NewArchiveStorage(nil))
    cacheInstance.refusal = errors.New("redis: connection refused")

    runErr := NewCacheClearCommand().Run(runtimeInstance, newBoolFlagContext("unused", false, &bytes.Buffer{}))
    if nil == runErr || false == strings.HasPrefix(runErr.Error(), "cache clear: clearing the cache did not complete") {
        t.Fatalf("expected the refusal under the command's own name, got %v", runErr)
    }

    if false == errors.Is(runErr, cacheInstance.refusal) {
        t.Errorf("expected the backend's refusal to stay the cause, got %v", runErr)
    }
}
