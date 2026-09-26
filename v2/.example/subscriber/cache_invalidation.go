package subscriber

import (
    "errors"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
)

/* deleteCacheEntries drops every given key and only then answers, joining the failures, so one failed delete never leaves the later entries, the list entry among them, serving a catalogue the database has dropped. A blank key is skipped, so a caller hands an optional spelling over as it is. */
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
