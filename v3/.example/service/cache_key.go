package service

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

func CacheKeyUserByUsername(username string) string {
    return cacheKeyUserByUsernamePrefix + "-" + username
}
