package config

import (
    "strings"
    "testing"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
)

func TestCacheKeyPrefix_CarriesTheLayoutTokenInsideTheNamespace(t *testing.T) {
    prefix := cacheKeyPrefix()

    if false == strings.HasPrefix(prefix, redisCacheKeyPrefixRoot) {
        t.Fatalf("expected the prefix to stay inside the namespace %q, got %q", redisCacheKeyPrefixRoot, prefix)
    }

    if redisCacheKeyPrefixRoot+examplecache.LayoutToken()+":" != prefix {
        t.Fatalf("expected the layout token between the namespace and the key, got %q", prefix)
    }
}
