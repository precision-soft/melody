package httpclient

import (
    "net/textproto"
    "sort"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

/* canonicalHeaderMap normalizes every key to its canonical header spelling and refuses two spellings that collapse onto one, which would otherwise leave the value sent, often a credential, to map iteration order. It is the constructor's door, so the refusal is a panic at the wiring; SetHeaders reads canonicalizeHeaderMap instead. */
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

/* canonicalHeaderKey is the one reader of the spelling rule, so a key stored by the constructor and by a setter land on the same entry. */
func canonicalHeaderKey(key string) string {
    return textproto.CanonicalMIMEHeaderKey(key)
}
