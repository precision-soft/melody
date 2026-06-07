package rueidis

import (
    "strings"
    "testing"
)

func hashTagOf(key string) string {
    start := strings.Index(key, "{")
    if -1 == start {
        return ""
    }

    end := strings.Index(key[start+1:], "}")
    if -1 == end {
        return ""
    }

    return key[start+1 : start+1+end]
}

func TestTokenStoreKeysShareHashTagForClusterColocation(t *testing.T) {
    store := &RedisTokenStore{prefix: "melody:token"}

    tokenTag := hashTagOf(store.tokenKey("abc"))
    userTag := hashTagOf(store.userKey("alice"))
    userPrefixTag := hashTagOf(store.userKeyPrefix())

    if "" == tokenTag {
        t.Fatalf("expected the token key to carry a hash tag, got %q", store.tokenKey("abc"))
    }

    if tokenTag != userTag || tokenTag != userPrefixTag {
        t.Fatalf(
            "expected every token-store key to share one hash tag for cluster co-location, got token=%q user=%q prefix=%q",
            tokenTag,
            userTag,
            userPrefixTag,
        )
    }
}
