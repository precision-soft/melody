package translation_test

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/translation"
)

func newTestManager() *translation.Manager {
    english := translation.NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")
    english.Add("messages", "inbox", "{count, plural, =0 {No messages} one {# message} other {# messages}}")
    english.Add("messages", "invite", "{gender, select, male {He invited you} female {She invited you} other {They invited you}}")

    romanian := translation.NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut, {name}!")

    return translation.NewManager("en", []string{"en"}, english, romanian)
}

func TestTrans_InterpolatesPlaceholder(t *testing.T) {
    manager := newTestManager()

    result := manager.Trans("greeting", map[string]any{"name": "Ada"}, "messages", "en")
    if "Hello, Ada!" != result {
        t.Fatalf("unexpected result: %q", result)
    }
}

func TestTrans_PluralExactAndCategoryWithPound(t *testing.T) {
    manager := newTestManager()

    zero := manager.Trans("inbox", map[string]any{"count": 0}, "messages", "en")
    if "No messages" != zero {
        t.Fatalf("unexpected zero result: %q", zero)
    }

    one := manager.Trans("inbox", map[string]any{"count": 1}, "messages", "en")
    if "1 message" != one {
        t.Fatalf("unexpected one result: %q", one)
    }

    many := manager.Trans("inbox", map[string]any{"count": 5}, "messages", "en")
    if "5 messages" != many {
        t.Fatalf("unexpected other result: %q", many)
    }
}

func TestTrans_Select(t *testing.T) {
    manager := newTestManager()

    female := manager.Trans("invite", map[string]any{"gender": "female"}, "messages", "en")
    if "She invited you" != female {
        t.Fatalf("unexpected female result: %q", female)
    }

    unknown := manager.Trans("invite", map[string]any{"gender": "robot"}, "messages", "en")
    if "They invited you" != unknown {
        t.Fatalf("unexpected fallback result: %q", unknown)
    }
}

func TestTrans_FallsBackToDefaultLocale(t *testing.T) {
    manager := newTestManager()

    result := manager.Trans("inbox", map[string]any{"count": 2}, "messages", "ro")
    if "2 messages" != result {
        t.Fatalf("expected fallback to en, got: %q", result)
    }
}

func TestTrans_ResolvesBaseLocale(t *testing.T) {
    manager := newTestManager()

    result := manager.Trans("greeting", map[string]any{"name": "Ana"}, "messages", "ro-RO")
    if "Salut, Ana!" != result {
        t.Fatalf("expected ro base locale, got: %q", result)
    }
}

func TestTrans_ReturnsMessageIdWhenMissing(t *testing.T) {
    manager := newTestManager()

    result := manager.Trans("does.not.exist", nil, "messages", "en")
    if "does.not.exist" != result {
        t.Fatalf("expected message id passthrough, got: %q", result)
    }
}

func TestTrans_RomanianPluralCategories(t *testing.T) {
    romanian := translation.NewMapCatalog("ro")
    romanian.Add("messages", "files", "{count, plural, one {# fișier} few {# fișiere} other {# de fișiere}}")

    manager := translation.NewManager("ro", nil, romanian)

    cases := map[int]string{
        1:  "1 fișier",
        2:  "2 fișiere",
        19: "19 fișiere",
        20: "20 de fișiere",
    }

    for count, expected := range cases {
        result := manager.Trans("files", map[string]any{"count": count}, "messages", "ro")
        if expected != result {
            t.Fatalf("ro count=%d: expected %q, got %q", count, expected, result)
        }
    }
}

func TestTrans_RussianPluralCategories(t *testing.T) {
    russian := translation.NewMapCatalog("ru")
    russian.Add("messages", "files", "{count, plural, one {# файл} few {# файла} many {# файлов} other {# файла}}")

    manager := translation.NewManager("ru", nil, russian)

    cases := map[int]string{
        1:  "1 файл",
        2:  "2 файла",
        5:  "5 файлов",
        11: "11 файлов",
        21: "21 файл",
        22: "22 файла",
    }

    for count, expected := range cases {
        result := manager.Trans("files", map[string]any{"count": count}, "messages", "ru")
        if expected != result {
            t.Fatalf("ru count=%d: expected %q, got %q", count, expected, result)
        }
    }
}

func TestTrans_PathologicallyNestedPluralDoesNotOverflow(t *testing.T) {
    /** Build a plural nested far beyond the interpolation depth cap; the guard must return rather
    than recurse until the stack is exhausted. */
    var builder strings.Builder
    const nesting = 200
    for index := 0; index < nesting; index++ {
        builder.WriteString("{count, plural, other {")
    }
    builder.WriteString("deep")
    for index := 0; index < nesting; index++ {
        builder.WriteString("}}")
    }

    catalog := translation.NewMapCatalog("en")
    catalog.Add("messages", "deep", builder.String())

    manager := translation.NewManager("en", nil, catalog)

    /** The assertion is simply that this returns without panicking on a stack overflow. */
    result := manager.Trans("deep", map[string]any{"count": 1}, "messages", "en")
    if "" == result {
        t.Fatalf("expected a non-empty result from the depth-bounded interpolation")
    }
}

func TestHasMessage(t *testing.T) {
    manager := newTestManager()

    if false == manager.HasMessage("greeting", "messages", "en") {
        t.Fatalf("expected greeting to exist")
    }

    if true == manager.HasMessage("nope", "messages", "en") {
        t.Fatalf("did not expect nope to exist")
    }
}
