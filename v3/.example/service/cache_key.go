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
