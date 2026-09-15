package cache

import (
    "regexp"
    "testing"
    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestLayoutToken_IsStableAndTwelveHexCharacters(t *testing.T) {
    first := LayoutToken()
    second := LayoutToken()

    if first != second {
        t.Fatalf("expected the token to be stable, got %q then %q", first, second)
    }

    if false == regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(first) {
        t.Fatalf("expected twelve hex characters, got %q", first)
    }
}

func TestLayoutTokenOf_MovesWhenAFieldIsAdded(t *testing.T) {
    before := layoutTokenOf([]any{&layoutProbeBefore{}})
    after := layoutTokenOf([]any{&layoutProbeAfter{}})

    if before == after {
        t.Fatalf("expected the token to move with the layout, both read %q", before)
    }
}

func TestLayoutDescriptionOf_LooksIntoTheStructsAFieldHolds(t *testing.T) {
    description := layoutDescriptionOf(reflectTypeOf(&layoutProbeHolder{}), map[reflectType]bool{})

    for _, expected := range []string{"Nested cache.layoutProbeBefore{Id string; Code string}", "Pointers cache.layoutProbeAfter{Id string; Code string; Rate float64}", "Stamped time.Time{}"} {
        if false == contains(description, expected) {
            t.Fatalf("expected the description to carry %q, got %q", expected, description)
        }
    }

    if true == contains(description, "hidden") {
        t.Fatalf("expected the unexported field to stay out of the description, got %q", description)
    }
}

func TestCachedValueList_CarriesEveryCachedEntity(t *testing.T) {
    expected := map[string]bool{}
    for _, value := range []any{&entity.Product{}, &entity.Category{}, &entity.Currency{}, &entity.User{}} {
        expected[reflectTypeOf(value).String()] = false
    }

    for _, value := range cachedValueList {
        expected[reflectTypeOf(value).String()] = true
    }

    for name, present := range expected {
        if false == present {
            t.Fatalf("expected %s on the cached value list", name)
        }
    }
}
