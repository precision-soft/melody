package repository

import (
    "testing"
)


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
