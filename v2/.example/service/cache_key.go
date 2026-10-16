package service

import (
    "strings"

    "github.com/precision-soft/melody/v2/.example/repository"
)

/* cacheSafeIdentifierMaximumBytes bounds a caller-supplied identifier so the composed key stays under the backends' 1024-byte key ceiling with room for every prefix above. */
const cacheSafeIdentifierMaximumBytes = 255

/* CacheSafeIdentifier reports whether a caller-supplied identifier can be embedded in a cache key: the backend grammar refuses a space or a newline inside a key and bounds its length, so an identifier it refuses names a row no write door admits, and a lookup answers it as absent instead of asking the cache. */
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

/* CacheKeyUserByUsername folds the username itself, so the service that fills the entry and the three listeners that drop it write and clear it under one key. */
func CacheKeyUserByUsername(username string) string {
    return cacheKeyUserByUsernamePrefix + "-" + repository.NormalizedUsername(username)
}
