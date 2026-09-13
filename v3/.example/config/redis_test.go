package config

import (
    "strings"
    "testing"

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
