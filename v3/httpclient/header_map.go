package httpclient

import (
    "net/textproto"
    "sort"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

func canonicalHeaderMap(headers map[string]string) map[string]string {
    if nil == headers {
        return map[string]string{}
    }

    canonical := make(map[string]string, len(headers))
    rawKeysByCanonicalKey := make(map[string]string, len(headers))

    for key, value := range headers {
        canonicalKey := canonicalHeaderKey(key)

        occupiedRawKey, occupied := rawKeysByCanonicalKey[canonicalKey]
        if true == occupied {
            conflictingKeys := []string{occupiedRawKey, key}
            sort.Strings(conflictingKeys)

            exception.Panic(
                exception.NewError(
                    "header keys collide after canonicalization",
                    exceptioncontract.Context{
                        "header":         canonicalKey,
                        "firstHeaderKey": conflictingKeys[0],
                        "otherHeaderKey": conflictingKeys[1],
                    },
                    nil,
                ),
            )
        }

        rawKeysByCanonicalKey[canonicalKey] = key
        canonical[canonicalKey] = value
    }

    return canonical
}

func canonicalHeaderKey(key string) string {
    return textproto.CanonicalMIMEHeaderKey(key)
}
