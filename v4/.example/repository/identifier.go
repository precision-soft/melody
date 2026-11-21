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

/* raisedFloor answers whichever of the floor and the identifier carries the higher numeric tail under the prefix: the in-memory repositories keep, under their lock, the highest identifier they ever stored, which the database keeps in its identifier sequence, so a deleted entity's identifier is not minted again */
func raisedFloor(floor string, identifier string, prefix string) string {
    if highestIdSuffix([]string{identifier}, prefix) > highestIdSuffix([]string{floor}, prefix) {
        return identifier
    }

    return floor
}

/* seededFloor is the floor a repository starts from: the highest identifier of the seed under the prefix, so deleting a seeded entity never hands its identifier to the next create */
func seededFloor(identifierList []string, prefix string) string {
    floor := ""
    for _, identifier := range identifierList {
        floor = raisedFloor(floor, identifier, prefix)
    }

    return floor
}

/* ErrIdentifierAtCeiling refuses a supplied identifier whose numeric tail is within identifierCeilingMargin of the ceiling: stored, it would raise the floor so close to the ceiling that the mints after it could reach it and collide for good. */
var ErrIdentifierAtCeiling = errors.New("the identifier's number is too close to the ceiling an identifier can carry")

/* identifierCeilingMargin is the room a supplied identifier must leave below the ceiling: a tail just under it would raise the floor there, and the mints after it would reach the ceiling and collide for good */
const identifierCeilingMargin = 1 << 32

/* refuseIdentifierAtCeiling answers ErrIdentifierAtCeiling for an identifier under the prefix whose numeric tail is within identifierCeilingMargin of the ceiling; an identifier without a numeric tail, or one too long to parse, never raises the floor and is left alone */
func refuseIdentifierAtCeiling(identifier string, prefix string) error {
    trimmed := strings.TrimSpace(identifier)
    if false == strings.HasPrefix(trimmed, prefix) {
        return nil
    }

    suffix, parseErr := strconv.ParseInt(strings.TrimPrefix(trimmed, prefix), 10, 64)
    if nil != parseErr {
        return nil
    }

    if math.MaxInt64-identifierCeilingMargin <= suffix {
        return ErrIdentifierAtCeiling
    }

    return nil
}
