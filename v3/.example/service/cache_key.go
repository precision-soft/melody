package service

import (
    "net/url"

    "github.com/precision-soft/melody/v3/.example/repository"
)

const (
    CacheKeyProductList  = "example-product-list"
    CacheKeyCategoryList = "example-category-list"
    CacheKeyCurrencyList = "example-currency-list"
    CacheKeyUserList     = "example-user-list"

    cacheKeyProductByIdPrefix    = "example-product-by-id"
    cacheKeyCategoryByIdPrefix   = "example-category-by-id"
    cacheKeyCurrencyByIdPrefix   = "example-currency-by-id"
    cacheKeyUserByIdPrefix       = "example-user-by-id"
    cacheKeyUserByUsernamePrefix = "example-user-by-username"
)

/* cacheSafeIdentifierMaximumBytes bounds a caller-supplied identifier so the composed key stays under the backends' 1024-byte key ceiling with room for every prefix above and for the escape below, which can triple a byte: the widest column such an identifier is stored in is 255 bytes, so a longer one names a row no write door admits. */
const cacheSafeIdentifierMaximumBytes = 255

/* CacheSafeIdentifier reports whether a caller-supplied identifier can be embedded in a cache key: the escape below keeps a space or a newline out of the key, but the key grammar also bounds its length, and an identifier long enough to breach it once escaped names a row that no write door admits — a lookup answers not-found instead of asking the cache a question it would refuse, which surfaced as a 500 on the anonymous login door for a name the user table cannot hold, and on the read of an id that simply does not exist. */
func CacheSafeIdentifier(identifier string) bool {
    if "" == identifier {
        return false
    }

    return cacheSafeIdentifierMaximumBytes >= len(identifier)
}

/* cacheKeyPart escapes the one part of a key that comes from outside.

   The cache contract states a key grammar — non-empty, no spaces, no newlines — and both backends refuse a
   key that breaks it, deliberately with the same words, so development and production agree. Every key
   below is built from a value a client chose: a username typed into the login form, an identifier taken
   from a path segment the router has already percent-decoded. Spliced in raw, `ad min` made the
   unauthenticated login door answer 500 where the same request with a well-formed name answers 401, and
   `/products/api/read/a%20b/` answered 500 where a merely absent identifier answers 404 — a lookup that
   should have missed became an error instead, on doors anyone can reach.

   Escaping rather than refusing is what keeps the two answers apart: a name with a space is a name this
   application does not have, not a request it cannot understand. The escape is reversible, so two different
   values cannot fold onto one key, and it leaves the ordinary identifier untouched, so the keys stay
   readable in a cache dump. */
func cacheKeyPart(value string) string {
    return url.PathEscape(value)
}

func CacheKeyProductById(id string) string {
    return cacheKeyProductByIdPrefix + "-" + cacheKeyPart(id)
}

func CacheKeyCategoryById(id string) string {
    return cacheKeyCategoryByIdPrefix + "-" + cacheKeyPart(id)
}

func CacheKeyCurrencyById(id string) string {
    return cacheKeyCurrencyByIdPrefix + "-" + cacheKeyPart(id)
}

func CacheKeyUserById(id string) string {
    return cacheKeyUserByIdPrefix + "-" + cacheKeyPart(id)
}

/* CacheKeyUserByUsername folds the username itself rather than trusting the caller to have done it. Four callers reach this key — the service that fills the entry and the three listeners that drop it — and every one of them used to spell the fold out again beside the call, so a single one of them drifting would have left the entry written under one key and cleared under another: a user who changed their name, or was deleted, would go on being served from the cache under the old one with nothing to say so. */
func CacheKeyUserByUsername(username string) string {
    return cacheKeyUserByUsernamePrefix + "-" + cacheKeyPart(repository.NormalizedUsername(username))
}
