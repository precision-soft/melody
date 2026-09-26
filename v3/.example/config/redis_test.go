package config

import (
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
)

/* the namespace root is what the three example applications keep apart; the layout token inside it is what keeps two BUILDS of this one apart, so a build reads only what a build of the same layout wrote */
func TestCacheKeyPrefix_CarriesTheLayoutTokenInsideTheNamespace(t *testing.T) {
    prefix := cacheKeyPrefix()

    if false == strings.HasPrefix(prefix, redisCacheKeyPrefixRoot) {
        t.Fatalf("expected the prefix to stay inside the namespace %q, got %q", redisCacheKeyPrefixRoot, prefix)
    }

    if redisCacheKeyPrefixRoot+examplecache.LayoutToken()+":" != prefix {
        t.Fatalf("expected the layout token between the namespace and the key, got %q", prefix)
    }
}

/* the write allowance is thirty catalogue writes a minute per address: a person editing the nomenclature
   never meets it and a script does. The limiter it is handed to is redis's and exposes neither number, so the
   two constants are what is pinned; the call site reads them. */
func TestCatalogWriteThrottle_AllowsThirtyWritesAMinute(t *testing.T) {
    if 30 != catalogWriteAllowance || time.Minute != catalogWriteWindow {
        t.Fatalf("expected thirty writes a minute, got %d per %s", catalogWriteAllowance, catalogWriteWindow)
    }
}
