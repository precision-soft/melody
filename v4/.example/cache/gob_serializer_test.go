package cache

import (
    "reflect"
    "regexp"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
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
    for _, value := range []any{&entity.Product{}, &entity.Category{}, &entity.Currency{}, &entity.UserProfile{}} {
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

type reflectType = reflect.Type

func reflectTypeOf(value any) reflect.Type {
    return reflect.TypeOf(value)
}

func contains(haystack string, needle string) bool {
    return strings.Contains(haystack, needle)
}

type layoutProbeMapHolder struct {
    ByCode map[string]layoutProbeBefore
}

func TestLayoutDescriptionOf_LooksThroughAMapToItsElement(t *testing.T) {
    description := layoutDescriptionOf(reflect.TypeOf(layoutProbeMapHolder{}), map[reflect.Type]bool{})

    if false == strings.Contains(description, "ByCode map[string]cache.layoutProbeBefore{Id string; Code string}") {
        t.Fatalf("expected the map's element to be described field by field, got %q", description)
    }
}

func TestGobSerializer_RefusesAWholeUser(t *testing.T) {
    if _, serializeErr := NewGobSerializer().Serialize(entity.NewUser("user-1", "user", "digest", []string{entity.RoleUser})); nil == serializeErr {
        t.Fatal("expected a whole account, digest included, refused by the serializer")
    }

    if _, serializeErr := NewGobSerializer().Serialize(entity.NewUser("user-1", "user", "digest", []string{entity.RoleUser}).Profile()); nil != serializeErr {
        t.Fatalf("expected the profile serialized, got %v", serializeErr)
    }
}

func TestLayoutToken_CoversTheUserProfile(t *testing.T) {
    withoutProfile := make([]any, 0, len(cachedValueList))
    for _, value := range cachedValueList {
        if _, isProfile := value.(*entity.UserProfile); false == isProfile {
            withoutProfile = append(withoutProfile, value)
        }
    }

    if len(cachedValueList) == len(withoutProfile) || LayoutToken() == layoutTokenOf(withoutProfile) {
        t.Fatal("expected the layout token to cover the user profile")
    }
}
