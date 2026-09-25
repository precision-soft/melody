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

/* cacheSafeIdentifierMaximumBytes keeps a composed key under the backends' 1024-byte ceiling with room for every prefix and for the escape, which can triple a byte; the widest column an identifier is stored in holds 255 bytes, so a longer one names no row. */
const cacheSafeIdentifierMaximumBytes = 255

/* CacheSafeIdentifier reports whether a caller-supplied identifier fits a cache key once escaped. An identifier that does not names a row no write door admits, so a lookup answers it as absent instead of asking the cache a question it would refuse. */
func CacheSafeIdentifier(identifier string) bool {
    if "" == identifier {
        return false
    }

    return cacheSafeIdentifierMaximumBytes >= len(identifier)
}

/* cacheKeyPart escapes the one part of a key a client chose, since both cache backends refuse a key carrying a space or a newline. Escaping rather than refusing answers a name this application does not have as absent; the escape is reversible, so two values never fold onto one key, and it leaves an ordinary identifier untouched. */
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

/* CacheKeyUserByUsername folds the username itself, so the service that fills the entry and the listeners that drop it always build the same key. */
func CacheKeyUserByUsername(username string) string {
    return cacheKeyUserByUsernamePrefix + "-" + cacheKeyPart(repository.NormalizedUsername(username))
}
