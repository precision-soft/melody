package repository

import (
    "math"
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

    /* the answer is capped one below the int64 ceiling: every caller mints the NEXT id as this plus one, and a stored suffix at the very ceiling would wrap that addition into a negative id — stored once, the wrapped id collides with itself on every later mint and empty-id creation is refused forever. Capped, the mint lands on the ceiling and collides with the existing row, which the caller reports as the ordinary "id already exists". */
    if math.MaxInt64-1 < highest {
        return math.MaxInt64 - 1
    }

    return highest
}
