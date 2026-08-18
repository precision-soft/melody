package service

import (
    "strings"
    "testing"
)

/* The key is what ties a cached user to the entries the listeners drop. Four callers reach it — the service that fills the entry and the three listeners that clear it — so the fold has to live HERE rather than beside each call: a caller that forgot it would write under one key and clear under another, and a renamed or deleted user would go on being served from the cache with nothing to say so. */
func TestCacheKeyUserByUsernameFoldsWhateverItIsGiven(t *testing.T) {
    expected := CacheKeyUserByUsername("user")

    for _, spelling := range []string{"USER", " user ", "User", "\tUSER\n"} {
        if expected != CacheKeyUserByUsername(spelling) {
            t.Fatalf("%q keys to %q, not to %q", spelling, CacheKeyUserByUsername(spelling), expected)
        }
    }
}

func TestCacheKeyUserByUsernameSeparatesDistinctUsers(t *testing.T) {
    if CacheKeyUserByUsername("user") == CacheKeyUserByUsername("editor") {
        t.Fatalf("two users share one key")
    }
}

/* Every builder carries its own prefix, so a product and a user of the same identifier cannot collide in one cache. */
func TestCacheKeysDoNotCollideAcrossKinds(t *testing.T) {
    keys := map[string]string{
        "product":  CacheKeyProductById("1"),
        "category": CacheKeyCategoryById("1"),
        "currency": CacheKeyCurrencyById("1"),
        "user":     CacheKeyUserById("1"),
    }

    seen := map[string]string{}
    for kind, key := range keys {
        if previous, exists := seen[key]; true == exists {
            t.Fatalf("%s and %s share the key %q", previous, kind, key)
        }

        seen[key] = kind
    }

    for _, listKey := range []string{CacheKeyProductList, CacheKeyCategoryList, CacheKeyCurrencyList, CacheKeyUserList} {
        for kind, key := range keys {
            if listKey == key {
                t.Fatalf("the %s list shares a key with a single %s", listKey, kind)
            }
        }
    }
}

/* the predicate mirrors the backend key grammar — no space, no newline, bounded length — so a finder can answer absent instead of asking the cache a question it would refuse with an error the door renders as a 500 */
func TestCacheSafeIdentifierMirrorsTheBackendKeyGrammar(t *testing.T) {
    refused := map[string]string{
        "empty":    "",
        "space":    "a b",
        "newline":  "a\nb",
        "oversize": strings.Repeat("x", 256),
    }

    for name, identifier := range refused {
        if true == CacheSafeIdentifier(identifier) {
            t.Fatalf("%s: expected %q refused", name, identifier)
        }
    }

    accepted := []string{"product-1", "café", strings.Repeat("x", 255)}
    for _, identifier := range accepted {
        if false == CacheSafeIdentifier(identifier) {
            t.Fatalf("expected %q accepted", identifier)
        }
    }
}
