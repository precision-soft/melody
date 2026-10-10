package repository

import (
    "errors"
    "math"
    "strconv"
    "strings"
)

/* highestIdSuffix answers the largest numeric tail among the identifiers that carry the prefix, or zero when none parses. */
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

    /* capped one below the int64 ceiling because every caller mints this plus one: a mint at the ceiling collides with the existing row and is refused as "id already exists" rather than wrapping into a negative id */
    if math.MaxInt64-1 < highest {
        return math.MaxInt64 - 1
    }

    return highest
}

/* ErrIdentifierAtCeiling refuses a supplied identifier whose numeric tail sits where a mint cannot pass it: stored, every later mint would collide with it for good. */
var ErrIdentifierAtCeiling = errors.New("the identifier's number is at the ceiling an identifier can carry")

/* refuseIdentifierAtCeiling answers ErrIdentifierAtCeiling for an identifier under the prefix whose numeric tail is the highest the mint caps at or above it; an identifier without a numeric tail, or one too long to parse, is never minted past and is left alone */
func refuseIdentifierAtCeiling(identifier string, prefix string) error {
    trimmed := strings.TrimSpace(identifier)
    if false == strings.HasPrefix(trimmed, prefix) {
        return nil
    }

    suffix, parseErr := strconv.ParseInt(strings.TrimPrefix(trimmed, prefix), 10, 64)
    if nil != parseErr {
        return nil
    }

    if math.MaxInt64-1 <= suffix {
        return ErrIdentifierAtCeiling
    }

    return nil
}
