package repository

import (
    "errors"
    "math"
    "testing"
)

/* The seeded currencies carry spelled-out identifiers — cur-eur, cur-usd — so an unparsable tail has to be skipped rather than read as zero: treated as zero it would still let the numbering start at one, but a tail read as any other number would hand out an identifier a seeded row already holds. */
func TestHighestIdSuffixSkipsWhatItCannotParse(t *testing.T) {
    highest := highestIdSuffix([]string{"cur-eur", "cur-usd", "cur-ron"}, "cur-")
    if 0 != highest {
        t.Fatalf("expected no numeric suffix to be found, got %d", highest)
    }

    if "cur-1" != nextCurrencyId([]string{"cur-eur", "cur-usd"}) {
        t.Fatalf("expected the first numeric currency identifier, got %q", nextCurrencyId([]string{"cur-eur", "cur-usd"}))
    }
}

func TestHighestIdSuffixReadsTheLargestTail(t *testing.T) {
    highest := highestIdSuffix([]string{"prod-1", " prod-9 ", "prod-3", "other-42"}, "prod-")
    if 9 != highest {
        t.Fatalf("expected the largest parsed suffix, got %d", highest)
    }

    if "prod-10" != nextProductId([]string{"prod-1", "prod-9"}) {
        t.Fatalf("expected the numbering to continue, got %q", nextProductId([]string{"prod-1", "prod-9"}))
    }
}

func TestHighestIdSuffixOnAnEmptyList(t *testing.T) {
    if 0 != highestIdSuffix(nil, "cat-") {
        t.Fatalf("expected zero for an empty list")
    }

    if "cat-1" != nextCategoryId(nil) {
        t.Fatalf("expected the first identifier, got %q", nextCategoryId(nil))
    }

    if "user-1" != nextUserId(nil) {
        t.Fatalf("expected the first identifier, got %q", nextUserId(nil))
    }
}

/* every caller mints the highest tail plus one, so a tail at the int64 ceiling is capped one below it: the mint then collides with the stored row instead of wrapping into a negative identifier */
func TestHighestIdSuffixCapsOneBelowTheCeiling(t *testing.T) {
    if highest := highestIdSuffix([]string{"prod-9223372036854775807"}, "prod-"); math.MaxInt64-1 != highest {
        t.Fatalf("expected the tail capped at MaxInt64-1, got %d", highest)
    }

    if next := nextProductId([]string{"prod-9223372036854775807"}); "prod-9223372036854775807" != next {
        t.Fatalf("expected the mint to stay at the ceiling rather than wrap, got %q", next)
    }
}

func TestRefuseIdentifierAtCeiling(t *testing.T) {
    for _, identifier := range []string{"prod-9223372036854775806", " prod-9223372036854775807 "} {
        if false == errors.Is(refuseIdentifierAtCeiling(identifier, "prod-"), ErrIdentifierAtCeiling) {
            t.Fatalf("%q: expected the ceiling refused", identifier)
        }
    }

    for _, identifier := range []string{"", "prod-9223372036854775805", "prod-99999999999999999999", "prod-x", "cat-9223372036854775807"} {
        if nil != refuseIdentifierAtCeiling(identifier, "prod-") {
            t.Fatalf("%q: expected no refusal", identifier)
        }
    }
}
