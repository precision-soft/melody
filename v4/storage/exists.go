package storage

import (
    "github.com/precision-soft/melody/v4/exception"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

/* Exists reports whether the key names an object the storage can serve: true when Stat answers, false when Stat answers ErrObjectNotFound or ErrKeyIsDirectory, and Stat's error beside false for anything else, a key the storage refuses included. */
func Exists(boundStorage Bound, key string) (bool, error) {
    _, statErr := boundStorage.Stat(key, storagecontract.StatOptions{})
    if nil == statErr {
        return true, nil
    }

    if true == exception.ChainIs(statErr, storagecontract.ErrObjectNotFound) || true == exception.ChainIs(statErr, storagecontract.ErrKeyIsDirectory) {
        return false, nil
    }

    return false, statErr
}
