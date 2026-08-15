package service

import (
    "strings"

    "github.com/precision-soft/melody/.example/repository"
)

/* cacheSafeIdentifierMaximumBytes bounds a caller-supplied identifier so the composed key stays under the backends' 1024-byte key ceiling with room for every prefix above. */
const cacheSafeIdentifierMaximumBytes = 255

/* CacheSafeIdentifier reports whether a caller-supplied identifier can be embedded in a cache key: the backend grammar refuses a space or a newline inside a key and bounds its length, so an identifier the grammar refuses names a row that no write door admits — a lookup answers not-found instead of asking the cache a question it would refuse, which used to surface as a 500 on the read of an id that simply does not exist. */
func CacheSafeIdentifier(identifier string) bool {
    if "" == identifier {
        return false
    }

    if true == strings.ContainsAny(identifier, " \n") {
        return false
    }

    return cacheSafeIdentifierMaximumBytes >= len(identifier)
}

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

func CacheKeyProductById(id string) string {
    return cacheKeyProductByIdPrefix + "-" + id
}

func CacheKeyCategoryById(id string) string {
    return cacheKeyCategoryByIdPrefix + "-" + id
}

func CacheKeyCurrencyById(id string) string {
    return cacheKeyCurrencyByIdPrefix + "-" + id
}

func CacheKeyUserById(id string) string {
    return cacheKeyUserByIdPrefix + "-" + id
}

/* CacheKeyUserByUsername folds the username itself rather than trusting the caller to have done it. Four callers reach this key — the service that fills the entry and the three listeners that drop it — and every one of them used to spell the fold out again beside the call, so a single one of them drifting would have left the entry written under one key and cleared under another: a user who changed their name, or was deleted, would go on being served from the cache under the old one with nothing to say so. */
func CacheKeyUserByUsername(username string) string {
    return cacheKeyUserByUsernamePrefix + "-" + repository.NormalizedUsername(username)
}
