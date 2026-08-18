package subscriber

import (
    "errors"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
)

/* deleteCacheEntries drops every given key and only then answers, joining the failures: the listeners invalidate several entries per change, and an early return on the first failure used to skip the later deletes — the list entry above all — leaving a ttl-less cache serving a catalogue the database no longer holds. A blank key is skipped rather than refused, so a caller with an optional spelling (an empty normalized username) hands it over as it is. */
func deleteCacheEntries(cacheInstance cachecontract.Cache, keys ...string) error {
    failures := make([]error, 0)

    for _, key := range keys {
        if "" == key {
            continue
        }

        deleteErr := cacheInstance.Delete(key)
        if nil != deleteErr {
            failures = append(failures, deleteErr)
        }
    }

    return errors.Join(failures...)
}
