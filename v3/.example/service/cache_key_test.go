package service

import (
    "strings"
    "testing"
)

func TestCacheKeysStayUsableForAValueTheClientChose(t *testing.T) {
    for _, value := range []string{"ad min", "a\nb", "a b c", " leading", "trailing "} {
        assertUsableAsCacheKey(t, CacheKeyUserByUsername(value))
        assertUsableAsCacheKey(t, CacheKeyUserById(value))
        assertUsableAsCacheKey(t, CacheKeyProductById(value))
        assertUsableAsCacheKey(t, CacheKeyCategoryById(value))
        assertUsableAsCacheKey(t, CacheKeyCurrencyById(value))
    }
}

func TestCacheKeysSeparateValuesThatDifferOnlyInWhitespace(t *testing.T) {
    first := CacheKeyUserById("a b")
    second := CacheKeyUserById("a+b")
    third := CacheKeyUserById("ab")

    if first == second || second == third || first == third {
        t.Fatalf("expected three distinct keys, got %q, %q and %q", first, second, third)
    }
}

func TestCacheKeysLeaveAnOrdinaryIdentifierAlone(t *testing.T) {
    if "example-user-by-id-user-3" != CacheKeyUserById("user-3") {
        t.Fatalf("expected an ordinary identifier to be spliced as it reads, got %q", CacheKeyUserById("user-3"))
    }

    if "example-user-by-username-admin" != CacheKeyUserByUsername("  ADMIN  ") {
        t.Fatalf("expected the username to be folded and spliced as it reads, got %q", CacheKeyUserByUsername("  ADMIN  "))
    }
}

func TestCacheSafeIdentifierBoundsTheLengthOfACallerChosenIdentifier(t *testing.T) {
    if false == CacheSafeIdentifier(strings.Repeat("a", 255)) || false == CacheSafeIdentifier("ad min") {
        t.Fatal("expected an identifier within the bound to be admitted, spaces included, since the key part escapes them")
    }

    if true == CacheSafeIdentifier(strings.Repeat("a", 256)) || true == CacheSafeIdentifier("") {
        t.Fatal("expected an identifier over the bound, and the empty one, to be refused")
    }
}
