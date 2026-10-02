package repository

import (
    "testing"
)

/* the next identifier continues the seeded numbering; an identifier whose tail is not a number is skipped rather than treated as zero, which is what lets the currencies keep their spelled-out identifiers without blocking a numeric one from ever being handed out */

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

/* a prefix that no identifier carries must not raise the count: an empty catalogue starts at one */

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

func TestRaisedFloorKeepsTheHigherTail(t *testing.T) {
    if "cur-7" != raisedFloor("cur-7", "cur-3", "cur-") {
        t.Fatalf("a lower identifier lowered the floor")
    }

    if "cur-9" != raisedFloor("cur-7", "cur-9", "cur-") {
        t.Fatalf("a higher identifier did not raise the floor")
    }

    if "cur-7" != raisedFloor("cur-7", "cur-eur", "cur-") {
        t.Fatalf("an identifier without a numeric tail moved the floor")
    }

    if "user-1" != raisedFloor("", "user-1", "user-") {
        t.Fatalf("the first identifier did not set the floor")
    }
}

func TestSeededFloor_IsTheHighestSeededIdentifier(t *testing.T) {
    if floor := seededFloor([]string{"prod-2", "prod-10", "prod-9", "other-99"}, "prod-"); "prod-10" != floor {
        t.Fatalf("expected prod-10, got %q", floor)
    }

    if floor := seededFloor(nil, "prod-"); "" != floor {
        t.Fatalf("expected no floor for no seed, got %q", floor)
    }
}
