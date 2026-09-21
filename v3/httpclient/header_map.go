package httpclient

import (
    "net/textproto"
    "sort"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* canonicalHeaderMap normalizes every key to its canonical header spelling and refuses two spellings that collapse onto one: the maps are applied to the request with Set, which canonicalizes, so x-api-key and X-Api-Key in one map stayed two entries whose survivor was chosen by map iteration order — a different value per request, in what is often a credential header. The serializer refuses colliding mime spellings at construction for the same reason. This is the constructor's door, so the refusal is a panic: a colliding map handed to the wiring is refused where the mistake is made. The request-time door, SetHeaders, reads canonicalizeHeaderMap instead and keeps the refusal for the request. */
func canonicalHeaderMap(headers map[string]string) map[string]string {
    canonical, err := canonicalizeHeaderMap(headers)
    if nil != err {
        exception.Panic(err)
    }

    return canonical
}

/* canonicalizeHeaderMap is the canonicalization with its refusal answered as an error: nil and the refusal on a collision, the canonical map otherwise. */
func canonicalizeHeaderMap(headers map[string]string) (map[string]string, *exception.Error) {
    if nil == headers {
        return map[string]string{}, nil
    }

    canonical := make(map[string]string, len(headers))
    rawKeysByCanonicalKey := make(map[string]string, len(headers))

    for key, value := range headers {
        canonicalKey := canonicalHeaderKey(key)

        occupiedRawKey, occupied := rawKeysByCanonicalKey[canonicalKey]
        if true == occupied {
            conflictingKeys := []string{occupiedRawKey, key}
            sort.Strings(conflictingKeys)

            return nil, exception.NewError(
                "header keys collide after canonicalization",
                exceptioncontract.Context{
                    "header":         canonicalKey,
                    "firstHeaderKey": conflictingKeys[0],
                    "otherHeaderKey": conflictingKeys[1],
                },
                nil,
            )
        }

        rawKeysByCanonicalKey[canonicalKey] = key
        canonical[canonicalKey] = value
    }

    return canonical, nil
}

/* canonicalHeaderKey is the one reader of the spelling rule: every door that writes into a header map goes through it, so a key stored by the constructor and the same key stored by a setter land on the same entry. */
func canonicalHeaderKey(key string) string {
    return textproto.CanonicalMIMEHeaderKey(key)
}
