package repository

import (
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

/* raisedFloor answers whichever of the floor and the identifier carries the higher numeric tail under the prefix: the in-memory repositories keep, under their lock, the highest identifier they ever stored, which the database keeps in its identifier sequence, so a deleted entity's identifier is not minted again */
func raisedFloor(floor string, identifier string, prefix string) string {
    if highestIdSuffix([]string{identifier}, prefix) > highestIdSuffix([]string{floor}, prefix) {
        return identifier
    }

    return floor
}
