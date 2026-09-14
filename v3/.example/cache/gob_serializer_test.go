package cache

import (
    "reflect"
    "regexp"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
)

type layoutProbeBefore struct {
    Id   string
    Code string
}

type layoutProbeAfter struct {
    Id   string
    Code string
    Rate float64
}

type layoutProbeHolder struct {
    Nested   layoutProbeBefore
    Stamped  time.Time
    Pointers []*layoutProbeAfter
    hidden   int
}

/* the token is what the prefix carries, so it has to be the same on every call of the same build and short enough to sit in every key */
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

/* a field added to a cached struct is exactly the change gob decodes silently to zero, so it is the change the token has to see */
func TestLayoutTokenOf_MovesWhenAFieldIsAdded(t *testing.T) {
    before := layoutTokenOf([]any{&layoutProbeBefore{}})
    after := layoutTokenOf([]any{&layoutProbeAfter{}})

    if before == after {
        t.Fatalf("expected the token to move with the layout, both read %q", before)
    }
}

/* a field of a NESTED struct is decoded by the same rule, so the description looks through pointers and slices into the structs they hold, names each exported field, and leaves the unexported ones — which gob does not encode — out */
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

/* the list the serializer registers is the list the token is computed over: every entity the application caches is on it, so a type cached but not tokened cannot exist */
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

/* small spellings so the tests above read as sentences */
type reflectType = reflect.Type

func reflectTypeOf(value any) reflect.Type {
    return reflect.TypeOf(value)
}

func contains(haystack string, needle string) bool {
    return strings.Contains(haystack, needle)
}
