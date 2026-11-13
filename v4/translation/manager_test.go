package translation

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

func newTestManager() *Manager {
    english := NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")
    english.Add("messages", "inbox", "{count, plural, =0 {No messages} one {# message} other {# messages}}")
    english.Add("messages", "invite", "{gender, select, male {He invited you} female {She invited you} other {They invited you}}")

    romanian := NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut, {name}!")

    return NewManager([]translationcontract.Catalog{english, romanian}, "en", []string{"en"})
}

/* verbatimDomainCatalog answers exactly the domain it is asked for, which the contract permits: the coercion of the empty domain is the manager door's to make, not an obligation of every catalog. */
type verbatimDomainCatalog struct {
    locale           string
    messagesByDomain map[string]map[string]string
}

func (instance *verbatimDomainCatalog) Locale() string {
    return instance.locale
}

func (instance *verbatimDomainCatalog) Get(messageId string, domain string) (string, bool) {
    messages, exists := instance.messagesByDomain[domain]
    if false == exists {
        return "", false
    }

    message, found := messages[messageId]
    return message, found
}

/* the empty domain resolves at the one door every catalog is asked through: a catalog that takes the contract at its word and answers the asked-for domain verbatim is still asked for the default, rather than for "" */
func TestTrans_EmptyDomainResolvesToTheDefaultAtTheManagerDoor(t *testing.T) {
    catalog := &verbatimDomainCatalog{
        locale: "en",
        messagesByDomain: map[string]map[string]string{
            DefaultDomain: {"greeting": "Hello!"},
        },
    }

    manager := NewManager([]translationcontract.Catalog{catalog}, "en", []string{"en"})

    if "Hello!" != manager.Trans("greeting", nil, "", "en") {
        t.Fatalf("expected the empty domain resolved to the default before the catalog was asked")
    }
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

func TestNewManager_NilCatalogsAnswersRawIds(t *testing.T) {
    manager := NewManager(nil, "en", nil)

    if "greeting" != manager.Trans("greeting", nil, "messages", "en") {
        t.Fatalf("expected a manager with no catalog to answer the raw id")
    }
}

func TestTrans_RomanianPluralCategories(t *testing.T) {
    romanian := NewMapCatalog("ro")
    romanian.Add("messages", "files", "{count, plural, one {# fișier} few {# fișiere} other {# de fișiere}}")

    manager := NewManager([]translationcontract.Catalog{romanian}, "ro", nil)

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
    russian := NewMapCatalog("ru")
    russian.Add("messages", "files", "{count, plural, one {# файл} few {# файла} many {# файлов} other {# файла}}")

    manager := NewManager([]translationcontract.Catalog{russian}, "ru", nil)

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

/* MapCatalog.Add refuses such a nesting, so the renderer's own bound is reached through a catalog that does not validate */
func TestTrans_PathologicallyNestedPluralDoesNotOverflow(t *testing.T) {
    var builder strings.Builder
    const nesting = 200
    for index := 0; index < nesting; index++ {
        builder.WriteString("{count, plural, other {")
    }
    builder.WriteString("deep")
    for index := 0; index < nesting; index++ {
        builder.WriteString("}}")
    }

    manager := NewManager([]translationcontract.Catalog{unvalidatedCatalog{locale: "en", message: builder.String()}}, "en", nil)

    result := manager.Trans("deep", map[string]any{"count": 1}, "messages", "en")
    if "" == result {
        t.Fatalf("expected a non-empty result from the depth-bounded interpolation")
    }
}

/* a missing plural argument stays visible as its placeholder, as the plain placeholder does */
func TestTrans_PluralWithMissingArgumentRendersTheVisiblePlaceholder(t *testing.T) {
    manager := newTestManager()

    result := manager.Trans("inbox", map[string]any{}, "messages", "en")
    if "{count}" != result {
        t.Fatalf("expected the absent count to stay visible, got: %q", result)
    }
}

/* a parameter present with a nil value is the caller saying so explicitly: the plural keeps rendering its other branch with an empty pound, only the ABSENT key renders as the placeholder */
func TestTrans_PluralWithAPresentNilArgumentFallsBackToOther(t *testing.T) {
    manager := newTestManager()

    result := manager.Trans("inbox", map[string]any{"count": nil}, "messages", "en")
    if " messages" != result {
        t.Fatalf("expected the other branch with an empty pound, got: %q", result)
    }
}

func TestTrans_SelectWithMissingArgumentRendersTheVisiblePlaceholder(t *testing.T) {
    manager := newTestManager()

    result := manager.Trans("invite", map[string]any{}, "messages", "en")
    if "{gender}" != result {
        t.Fatalf("expected the absent keyword to stay visible, got: %q", result)
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

func TestNewManager_RefusesANilCatalog(t *testing.T) {
    /* a nil catalog is refused, since it would build a translator answering raw ids for a whole locale */
    testhelper.AssertPanicsWithError(t, func() {
        NewManager([]translationcontract.Catalog{nil}, "en", nil)
    }, "translation catalog is nil")
}

/* the catalogs of a locale are asked in the order given, the first to answer winning */
func TestNewManager_TwoCatalogsOfOneLocaleAreAskedInOrder(t *testing.T) {
    first := NewMapCatalog("en")
    first.Add("messages", "greeting", "Hello")
    first.Add("messages", "shared", "from the first")

    second := NewMapCatalog("en")
    second.Add("messages", "farewell", "Bye")
    second.Add("messages", "shared", "from the second")

    manager := NewManager([]translationcontract.Catalog{first, second}, "en", nil)

    if "Hello" != manager.Trans("greeting", nil, "messages", "en") {
        t.Fatalf("expected the first catalog to keep answering, got %q", manager.Trans("greeting", nil, "messages", "en"))
    }

    if "Bye" != manager.Trans("farewell", nil, "messages", "en") {
        t.Fatalf("expected the second catalog to answer what the first does not hold, got %q", manager.Trans("farewell", nil, "messages", "en"))
    }

    if "from the first" != manager.Trans("shared", nil, "messages", "en") {
        t.Fatalf("expected the first catalog to win a message both hold, got %q", manager.Trans("shared", nil, "messages", "en"))
    }
}

/* the locale chain never asks for the empty locale, so a catalog whose Locale is empty could never be found: it is refused as the nil one is, instead of being stored under a key nothing reads */
func TestNewManager_RefusesACatalogWithoutALocale(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewManager([]translationcontract.Catalog{NewMapCatalog("")}, "en", nil)
    }, "translation catalog carries no locale")
}

/* the default and the fallback locales are asked by their exact name, so one no catalog carries would answer raw ids for every message the request locale misses */
func TestNewManager_RefusesALocaleNoCatalogCarries(t *testing.T) {
    cases := map[string]struct {
        defaultLocale   string
        fallbackLocales []string
        message         string
    }{
        "empty default":                 {"", nil, "translation default locale is empty"},
        "default without a catalog":     {"fr", nil, "translation default locale is carried by no catalog"},
        "default is only a region form": {"en-US", nil, "translation default locale is carried by no catalog"},
        "fallback without a catalog":    {"en", []string{"en", "de"}, "translation fallback locale is carried by no catalog"},
        "empty fallback":                {"en", []string{""}, "translation fallback locale is empty"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, func() {
                NewManager([]translationcontract.Catalog{NewMapCatalog("en"), NewMapCatalog("ro")}, testCase.defaultLocale, testCase.fallbackLocales)
            }, testCase.message)
        })
    }
}

func TestNewManager_TheRefusalNamesTheFallbackAndItsIndex(t *testing.T) {
    defer func() {
        recovered := recover()
        exceptionError, isExceptionError := recovered.(*exception.Error)
        if false == isExceptionError {
            t.Fatalf("expected an exception error, got %#v", recovered)
        }

        context := exceptionError.Context()
        if "de" != context["fallbackLocale"] || 1 != context["index"] {
            t.Fatalf("expected the refusal to name de at index 1, got %v", context)
        }
    }()

    NewManager([]translationcontract.Catalog{NewMapCatalog("en")}, "en", []string{"en", "de"})
}

/* with no catalog there is no locale to check against */
func TestNewManager_WithNoCatalogAdmitsAnyLocale(t *testing.T) {
    manager := NewManager(nil, "", []string{"de"})

    if "greeting" != manager.Trans("greeting", nil, "messages", "fr") {
        t.Fatalf("expected the raw id, got %q", manager.Trans("greeting", nil, "messages", "fr"))
    }
}
