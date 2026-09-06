package service

import (
    "strings"
    "testing"
)

/* the grammar both cache backends refuse on: a key with a space or a newline in it is an error, not a miss */
func assertUsableAsCacheKey(t *testing.T, key string) {
    t.Helper()

    if "" == key {
        t.Fatalf("expected a non-empty cache key")
    }

    if true == strings.Contains(key, " ") {
        t.Fatalf("expected the key to carry no space, got %q", key)
    }

    if true == strings.Contains(key, "\n") {
        t.Fatalf("expected the key to carry no newline, got %q", key)
    }
}

/* every one of these keys is built from something a client chose — a username typed into the login form, an
   identifier lifted from a path segment the router has already decoded — and the cache refuses a key that
   breaks its grammar. Spliced in raw, the refusal reached the caller as a 500 from doors that should have
   answered 401 and 404. */
func TestCacheKeysStayUsableForAValueTheClientChose(t *testing.T) {
    for _, value := range []string{"ad min", "a\nb", "a b c", " leading", "trailing "} {
        assertUsableAsCacheKey(t, CacheKeyUserByUsername(value))
        assertUsableAsCacheKey(t, CacheKeyUserById(value))
        assertUsableAsCacheKey(t, CacheKeyProductById(value))
        assertUsableAsCacheKey(t, CacheKeyCategoryById(value))
        assertUsableAsCacheKey(t, CacheKeyCurrencyById(value))
    }
}

/* the escape has to be reversible, or two accounts would share one entry and one of them would be served
   the other's answer — a worse defect than the one being repaired. */
func TestCacheKeysSeparateValuesThatDifferOnlyInWhitespace(t *testing.T) {
    first := CacheKeyUserById("a b")
    second := CacheKeyUserById("a+b")
    third := CacheKeyUserById("ab")

    if first == second || second == third || first == third {
        t.Fatalf("expected three distinct keys, got %q, %q and %q", first, second, third)
    }
}

/* the ordinary identifier is left as it reads, so a cache dump stays legible and the keys this application
   has always written keep their shape. */
func TestCacheKeysLeaveAnOrdinaryIdentifierAlone(t *testing.T) {
    if "example-user-by-id-user-3" != CacheKeyUserById("user-3") {
        t.Fatalf("expected an ordinary identifier to be spliced as it reads, got %q", CacheKeyUserById("user-3"))
    }

    if "example-user-by-username-admin" != CacheKeyUserByUsername("  ADMIN  ") {
        t.Fatalf("expected the username to be folded and spliced as it reads, got %q", CacheKeyUserByUsername("  ADMIN  "))
    }
}
