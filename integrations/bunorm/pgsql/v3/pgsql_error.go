package pgsql

import (
    "strings"
)

func IsDuplicateKey(err error) bool {
    if nil == err {
        return false
    }

    errMsg := err.Error()

    return strings.Contains(errMsg, "23505") || strings.Contains(errMsg, "duplicate key")
}
