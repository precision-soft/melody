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

const cacheSafeIdentifierMaximumBytes = 255

/* CacheSafeIdentifier reports whether a caller-supplied identifier can be embedded in a cache key: the escape below keeps a space or a newline out of the key, but the key grammar also bounds its length, and an identifier long enough to breach it once escaped names a row that no write door admits — a lookup answers not-found instead of asking the cache a question it would refuse, which surfaced as a 500 on the anonymous login door for a name the user table cannot hold, and on the read of an id that simply does not exist. */
func CacheSafeIdentifier(identifier string) bool {
    if "" == identifier {
        return false
    }

    return cacheSafeIdentifierMaximumBytes >= len(identifier)
}

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

/* CacheKeyUserByUsername normalizes the username so cache reads and invalidation use the same key. */
func CacheKeyUserByUsername(username string) string {
    return cacheKeyUserByUsernamePrefix + "-" + cacheKeyPart(repository.NormalizedUsername(username))
}
