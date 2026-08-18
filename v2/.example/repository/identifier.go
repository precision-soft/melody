package repository

import (
    "strconv"
    "strings"
)

/* highestIdSuffix reads the numeric tail of every identifier that carries the given prefix and reports the largest one it could parse, or zero when none of them was a number. The four repositories number their rows the same way and both implementations of each need the answer, so the walk lives here once. */
func highestIdSuffix(identifierList []string, prefix string) int64 {
    highest := int64(0)

    for _, identifier := range identifierList {
        trimmed := strings.TrimSpace(identifier)
        if false == strings.HasPrefix(trimmed, prefix) {
            continue
        }

        suffix, parseErr := strconv.ParseInt(strings.TrimPrefix(trimmed, prefix), 10, 64)
        if nil != parseErr {
            continue
        }

        if suffix > highest {
            highest = suffix
        }
    }

    return highest
}
