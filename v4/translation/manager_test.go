package translation

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

func mustTranslate(t *testing.T, translator translationcontract.Translator, messageId string, config translationcontract.TranslateConfig) string {
    t.Helper()

    message, translateErr := translator.Translate(messageId, config)
    if nil != translateErr {
        t.Fatalf("expected %q translated without an error, got %v", messageId, translateErr)
    }

    return message
}

func expectMessageNotFound(t *testing.T, translator translationcontract.Translator, messageId string, config translationcontract.TranslateConfig) {
    t.Helper()

    message, translateErr := translator.Translate(messageId, config)
    if messageId != message {
        t.Fatalf("expected the message id %q answered, got %q", messageId, message)
    }

    if false == errors.Is(translateErr, ErrMessageNotFound) {
        t.Fatalf("expected ErrMessageNotFound, got %v", translateErr)
    }
}

func newTestManager() *Manager {
    english := NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")
    english.Add("messages", "inbox", "{count, plural, =0 {No messages} one {# message} other {# messages}}")
    english.Add("messages", "invite", "{gender, select, male {He invited you} female {She invited you} other {They invited you}}")

    romanian := NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut, {name}!")

    return NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{english, romanian}, FallbackLocales: []string{"en"}})
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
func TestTranslate_EmptyDomainResolvesToTheDefaultAtTheManagerDoor(t *testing.T) {
    catalog := &verbatimDomainCatalog{
        locale: "en",
        messagesByDomain: map[string]map[string]string{
            DefaultDomain: {"greeting": "Hello!"},
        },
    }

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{catalog}, FallbackLocales: []string{"en"}})

    if "Hello!" != mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Locale: "en"}) {
        t.Fatalf("expected the empty domain resolved to the default before the catalog was asked")
    }
}

func TestTranslate_InterpolatesPlaceholder(t *testing.T) {
    manager := newTestManager()

    result := mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Parameters: map[string]any{"name": "Ada"}, Domain: "messages", Locale: "en"})
    if "Hello, Ada!" != result {
        t.Fatalf("unexpected result: %q", result)
    }
}

func TestTranslate_PluralExactAndCategoryWithPound(t *testing.T) {
    manager := newTestManager()

    zero := mustTranslate(t, manager, "inbox", translationcontract.TranslateConfig{Parameters: map[string]any{"count": 0}, Domain: "messages", Locale: "en"})
    if "No messages" != zero {
        t.Fatalf("unexpected zero result: %q", zero)
    }

    one := mustTranslate(t, manager, "inbox", translationcontract.TranslateConfig{Parameters: map[string]any{"count": 1}, Domain: "messages", Locale: "en"})
    if "1 message" != one {
        t.Fatalf("unexpected one result: %q", one)
    }

    many := mustTranslate(t, manager, "inbox", translationcontract.TranslateConfig{Parameters: map[string]any{"count": 5}, Domain: "messages", Locale: "en"})
    if "5 messages" != many {
        t.Fatalf("unexpected other result: %q", many)
    }
}

func TestTranslate_Select(t *testing.T) {
    manager := newTestManager()

    female := mustTranslate(t, manager, "invite", translationcontract.TranslateConfig{Parameters: map[string]any{"gender": "female"}, Domain: "messages", Locale: "en"})
    if "She invited you" != female {
        t.Fatalf("unexpected female result: %q", female)
    }

    unknown := mustTranslate(t, manager, "invite", translationcontract.TranslateConfig{Parameters: map[string]any{"gender": "robot"}, Domain: "messages", Locale: "en"})
    if "They invited you" != unknown {
        t.Fatalf("unexpected fallback result: %q", unknown)
    }
}

func TestTranslate_FallsBackToDefaultLocale(t *testing.T) {
    manager := newTestManager()

    result := mustTranslate(t, manager, "inbox", translationcontract.TranslateConfig{Parameters: map[string]any{"count": 2}, Domain: "messages", Locale: "ro"})
    if "2 messages" != result {
        t.Fatalf("expected fallback to en, got: %q", result)
    }
}

func TestTranslate_ResolvesBaseLocale(t *testing.T) {
    manager := newTestManager()

    result := mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Parameters: map[string]any{"name": "Ana"}, Domain: "messages", Locale: "ro-RO"})
    if "Salut, Ana!" != result {
        t.Fatalf("expected ro base locale, got: %q", result)
    }
}

func TestTranslate_AMessageNoCatalogCarriesAnswersTheIdAndErrMessageNotFound(t *testing.T) {
    manager := newTestManager()

    expectMessageNotFound(t, manager, "does.not.exist", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"})

    _, translateErr := manager.Translate("does.not.exist", translationcontract.TranslateConfig{Locale: "ro"})
    context := exception.FromError(translateErr).Context()
    if "does.not.exist" != context["messageId"] || DefaultDomain != context["domain"] || "ro" != context["locale"] {
        t.Fatalf("expected the refusal to name the message, the domain and the locale asked, got %v", context)
    }

    if true == manager.HasMessage("does.not.exist", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}) {
        t.Fatal("expected HasMessage false for a message no catalog carries")
    }
}

func TestTranslate_TheThreeOutcomesAreToldApart(t *testing.T) {
    english := NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{english, unvalidatedCatalog{locale: "en", message: "{g, select, male {he}}"}}, FallbackLocales: []string{"en"}})

    message, translateErr := manager.Translate("greeting", translationcontract.TranslateConfig{Parameters: map[string]any{"name": "Ada"}})
    if nil != translateErr || "Hello, Ada!" != message {
        t.Fatalf("expected the rendered message and no error, got %q, %v", message, translateErr)
    }

    message, translateErr = manager.Translate("pronoun", translationcontract.TranslateConfig{})
    if "" != message || nil == translateErr || true == errors.Is(translateErr, ErrMessageNotFound) {
        t.Fatalf("expected an empty message and the malformed pattern error, got %q, %v", message, translateErr)
    }
}

func TestNewManager_NilCatalogsAnswersRawIds(t *testing.T) {
    manager := NewManager(ManagerConfig{FallbackLocales: []string{"en"}})

    expectMessageNotFound(t, manager, "greeting", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"})
}

func TestTranslate_RomanianPluralCategories(t *testing.T) {
    romanian := NewMapCatalog("ro")
    romanian.Add("messages", "files", "{count, plural, one {# fișier} few {# fișiere} other {# de fișiere}}")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{romanian}, FallbackLocales: []string{"ro"}})

    cases := map[int]string{
        1:  "1 fișier",
        2:  "2 fișiere",
        19: "19 fișiere",
        20: "20 de fișiere",
    }

    for count, expected := range cases {
        result := mustTranslate(t, manager, "files", translationcontract.TranslateConfig{Parameters: map[string]any{"count": count}, Domain: "messages", Locale: "ro"})
        if expected != result {
            t.Fatalf("ro count=%d: expected %q, got %q", count, expected, result)
        }
    }
}

func TestTranslate_RussianPluralCategories(t *testing.T) {
    russian := NewMapCatalog("ru")
    russian.Add("messages", "files", "{count, plural, one {# файл} few {# файла} many {# файлов} other {# файла}}")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{russian}, FallbackLocales: []string{"ru"}})

    cases := map[int]string{
        1:  "1 файл",
        2:  "2 файла",
        5:  "5 файлов",
        11: "11 файлов",
        21: "21 файл",
        22: "22 файла",
    }

    for count, expected := range cases {
        result := mustTranslate(t, manager, "files", translationcontract.TranslateConfig{Parameters: map[string]any{"count": count}, Domain: "messages", Locale: "ru"})
        if expected != result {
            t.Fatalf("ru count=%d: expected %q, got %q", count, expected, result)
        }
    }
}

/* MapCatalog.Add refuses such a nesting, so the judge Translate runs before rendering is reached through a catalog that does not validate */
func TestTranslate_PathologicallyNestedPluralIsAnErrorWithoutOverflow(t *testing.T) {
    var builder strings.Builder
    const nesting = 200
    for index := 0; index < nesting; index++ {
        builder.WriteString("{count, plural, other {")
    }
    builder.WriteString("deep")
    for index := 0; index < nesting; index++ {
        builder.WriteString("}}")
    }

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{unvalidatedCatalog{locale: "en", message: builder.String()}}, FallbackLocales: []string{"en"}})

    result, translateErr := manager.Translate("deep", translationcontract.TranslateConfig{Parameters: map[string]any{"count": 1}, Domain: "messages", Locale: "en"})
    if "" != result || nil == translateErr {
        t.Fatalf("expected an empty result and the nesting refused, got %q, %v", result, translateErr)
    }

    if patternDefectNestedTooDeep != exception.FromError(translateErr).Context()["reason"] {
        t.Fatalf("expected the nesting named as the reason, got %v", exception.FromError(translateErr).Context())
    }
}

/* a missing plural argument stays visible as its placeholder, as the plain placeholder does */
func TestTranslate_PluralWithMissingArgumentRendersTheVisiblePlaceholder(t *testing.T) {
    manager := newTestManager()

    result := mustTranslate(t, manager, "inbox", translationcontract.TranslateConfig{Parameters: map[string]any{}, Domain: "messages", Locale: "en"})
    if "{count}" != result {
        t.Fatalf("expected the absent count to stay visible, got: %q", result)
    }
}

/* a parameter present with a nil value is the caller saying so explicitly: the plural keeps rendering its other branch with an empty pound, only the ABSENT key renders as the placeholder */
func TestTranslate_PluralWithAPresentNilArgumentFallsBackToOther(t *testing.T) {
    manager := newTestManager()

    result := mustTranslate(t, manager, "inbox", translationcontract.TranslateConfig{Parameters: map[string]any{"count": nil}, Domain: "messages", Locale: "en"})
    if " messages" != result {
        t.Fatalf("expected the other branch with an empty pound, got: %q", result)
    }
}

func TestTranslate_SelectWithMissingArgumentRendersTheVisiblePlaceholder(t *testing.T) {
    manager := newTestManager()

    result := mustTranslate(t, manager, "invite", translationcontract.TranslateConfig{Parameters: map[string]any{}, Domain: "messages", Locale: "en"})
    if "{gender}" != result {
        t.Fatalf("expected the absent keyword to stay visible, got: %q", result)
    }
}

func TestHasMessage_ReadsDomainAndLocaleFromTheConfigAndIgnoresParameters(t *testing.T) {
    english := NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello")
    english.Add("mail", "subject", "Welcome")
    romanian := NewMapCatalog("ro")
    romanian.Add("messages", "only.ro", "Doar")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{english, romanian}, FallbackLocales: []string{"en"}})

    cases := map[string]struct {
        messageId string
        config    translationcontract.TranslateConfig
        expected  bool
    }{
        "the zero config reads the default domain":   {"greeting", translationcontract.TranslateConfig{}, true},
        "a message of another domain":                {"greeting", translationcontract.TranslateConfig{Domain: "mail"}, false},
        "the domain the config names":                {"subject", translationcontract.TranslateConfig{Domain: "mail"}, true},
        "the default domain lacks the mail message":  {"subject", translationcontract.TranslateConfig{}, false},
        "the locale the config names":                {"only.ro", translationcontract.TranslateConfig{Locale: "ro"}, true},
        "the default locale lacks the ro message":    {"only.ro", translationcontract.TranslateConfig{Locale: "en"}, false},
        "parameters are not read":                    {"greeting", translationcontract.TranslateConfig{Parameters: map[string]any{"name": "Ada"}}, true},
        "a missing message":                          {"nope", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}, false},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            if testCase.expected != manager.HasMessage(testCase.messageId, testCase.config) {
                t.Fatalf("expected HasMessage %v", testCase.expected)
            }
        })
    }
}

func TestTranslate_TheZeroConfigReadsTheDefaultDomainAndTheDefaultLocale(t *testing.T) {
    english := NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello")
    english.Add("mail", "greeting", "Dear")
    romanian := NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{romanian, english}, FallbackLocales: []string{"en"}})

    if "Hello" != mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{}) {
        t.Fatal("expected the zero config to read the messages domain of the default locale")
    }

    if "Dear" != mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Domain: "mail", Locale: "ro"}) {
        t.Fatal("expected the domain read as the domain and the locale as the locale")
    }

    if "Salut" != mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Locale: "ro"}) {
        t.Fatal("expected the locale the config names")
    }
}

func TestNewManager_RefusesANilCatalog(t *testing.T) {
    /* a nil catalog is refused, since it would build a translator answering raw ids for a whole locale */
    testhelper.AssertPanicsWithError(t, func() {
        NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{nil}, FallbackLocales: []string{"en"}})
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

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{first, second}, FallbackLocales: []string{"en"}})

    if "Hello" != mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}) {
        t.Fatalf("expected the first catalog to keep answering, got %q", mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}))
    }

    if "Bye" != mustTranslate(t, manager, "farewell", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}) {
        t.Fatalf("expected the second catalog to answer what the first does not hold, got %q", mustTranslate(t, manager, "farewell", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}))
    }

    if "from the first" != mustTranslate(t, manager, "shared", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}) {
        t.Fatalf("expected the first catalog to win a message both hold, got %q", mustTranslate(t, manager, "shared", translationcontract.TranslateConfig{Domain: "messages", Locale: "en"}))
    }
}

/* the locale chain never asks for the empty locale, so a catalog whose Locale is empty could never be found: it is refused as the nil one is, instead of being stored under a key nothing reads */
func TestNewManager_RefusesACatalogWithoutALocale(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("")}, FallbackLocales: []string{"en"}})
    }, "translation catalog carries no locale")
}

/* the fallback locales are asked by their exact name, so one no catalog carries would answer raw ids for every message the request locale misses */
func TestNewManager_RefusesALocaleNoCatalogCarries(t *testing.T) {
    cases := map[string]struct {
        fallbackLocales []string
        message         string
    }{
        "fallback without a catalog":     {[]string{"en", "de"}, "translation fallback locale is carried by no catalog"},
        "fallback is only a region form": {[]string{"en-US"}, "translation fallback locale is carried by no catalog"},
        "empty fallback":                 {[]string{""}, "translation fallback locale is empty"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, func() {
                NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("en"), NewMapCatalog("ro")}, FallbackLocales: testCase.fallbackLocales})
            }, testCase.message)
        })
    }
}

func TestManager_NothingIsAskedAfterTheLastFallback(t *testing.T) {
    english := NewMapCatalog("en")
    english.Add("messages", "only.english", "english")

    romanian := NewMapCatalog("ro")
    romanian.Add("messages", "only.romanian", "romanian")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{english, romanian}, FallbackLocales: []string{"ro"}})

    if "romanian" != mustTranslate(t, manager, "only.romanian", translationcontract.TranslateConfig{Domain: "messages", Locale: "fr-FR"}) {
        t.Fatalf("expected the fallback to answer a locale no catalog carries")
    }

    message, translateErr := manager.Translate("only.english", translationcontract.TranslateConfig{Domain: "messages", Locale: "fr-FR"})
    if false == errors.Is(translateErr, ErrMessageNotFound) || "only.english" != message {
        t.Fatalf("expected no locale asked after the last fallback, got %q %v", message, translateErr)
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
        if "de" != context["fallbackLocale"] || 1 != context["index"] || "ManagerConfig.FallbackLocales" != context["field"] {
            t.Fatalf("expected the refusal to name de at index 1 and the config field, got %v", context)
        }
    }()

    NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("en")}, FallbackLocales: []string{"en", "de"}})
}

/* with no catalog there is no locale to check against */
func TestNewManager_WithNoCatalogAdmitsAnyLocale(t *testing.T) {
    manager := NewManager(ManagerConfig{FallbackLocales: []string{"de"}})

    expectMessageNotFound(t, manager, "greeting", translationcontract.TranslateConfig{Domain: "messages", Locale: "fr"})
}

func TestTranslate_ALocaleIsMatchedInBcp47Case(t *testing.T) {
    romanian := NewMapCatalog("ro-RO")
    romanian.Add("messages", "greeting", "Salut")
    chinese := NewMapCatalog("zh-Hant-TW")
    chinese.Add("messages", "greeting", "你好")
    english := NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{romanian, chinese, english}, FallbackLocales: []string{"RO-ro", "EN"}})

    for locale, expected := range map[string]string{"ro-ro": "Salut", "RO-RO": "Salut", "ZH-HANT-tw": "你好", "En": "Hello", "fr": "Salut"} {
        if result := mustTranslate(t, manager, "greeting", translationcontract.TranslateConfig{Domain: "messages", Locale: locale}); expected != result {
            t.Fatalf("expected %q for %q, got %q", expected, locale, result)
        }
    }

    withoutFallback := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{romanian, english}, FallbackLocales: []string{"en"}})
    if true == withoutFallback.HasMessage("greeting", translationcontract.TranslateConfig{Domain: "messages", Locale: "ro_ro"}) && "Salut" == mustTranslate(t, withoutFallback, "greeting", translationcontract.TranslateConfig{Domain: "messages", Locale: "ro_ro"}) {
        t.Fatal("expected an underscore not folded to a hyphen")
    }
}

func TestNewManager_RefusesTwoSpellingsOfOneLocale(t *testing.T) {
    defer func() {
        recovered := recover()
        exceptionError, isExceptionError := recovered.(*exception.Error)
        if false == isExceptionError {
            t.Fatalf("expected an exception error, got %#v", recovered)
        }

        context := exceptionError.Context()
        if "ro-RO" != context["locale"] || "ro-ro" != context["otherSpelling"] {
            t.Fatalf("expected both spellings named, got %v", context)
        }
    }()

    NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("ro-RO"), NewMapCatalog("ro-ro")}, FallbackLocales: []string{"ro-RO"}})
}

func TestNewManager_AssemblesOneLocaleFromCatalogsSpellingItAlike(t *testing.T) {
    first := NewMapCatalog("ro")
    first.Add("messages", "a", "A")
    second := NewMapCatalog("ro")
    second.Add("messages", "b", "B")

    manager := NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{first, second}, FallbackLocales: []string{"ro"}})
    if "B" != mustTranslate(t, manager, "b", translationcontract.TranslateConfig{Domain: "messages", Locale: "RO"}) {
        t.Fatal("expected the second catalog of the locale asked")
    }
}

func TestCanonicalLocale(t *testing.T) {
    for input, expected := range map[string]string{"": "", "EN": "en", "en-us": "en-US", "sr-latn-rs": "sr-Latn-RS", "es-419": "es-419", "de_de": "de_DE", "en-US-POSIX": "en-US-posix", "x": "x", "en-A1B2": "en-a1b2", "en-X1": "en-x1", "de-1901": "de-1901", "en-": "en-", "-us": "-US", "en--us": "en--US", "en-u-CA-gregory": "en-u-ca-gregory", "en-x-latn": "en-x-latn", "x-ab": "x-ab", "de-de-u-co-phonebk": "de-DE-u-co-phonebk", "\u212ao": "\u212ao", "x\xff": "x\xff", "Äb-ÄB": "Äb-Äb"} {
        if result := canonicalLocale(input); expected != result {
            t.Fatalf("expected %q for %q, got %q", expected, input, result)
        }
    }
}

func expectRefusalContext(t *testing.T, key string, expected string, build func()) {
    t.Helper()

    defer func() {
        recovered := recover()
        exceptionError, isExceptionError := recovered.(*exception.Error)
        if false == isExceptionError || expected != exceptionError.Context()[key] {
            t.Fatalf("expected %s named %q as written, got %#v", key, expected, recovered)
        }
    }()

    build()
}

func TestNewManager_TheFallbackRefusalsNameTheLocaleAsWritten(t *testing.T) {
    expectRefusalContext(t, "fallbackLocale", "RO_md", func() {
        NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("en")}, FallbackLocales: []string{"RO_md"}})
    })

    expectRefusalContext(t, "fallbackLocale", "DE-de", func() {
        NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("en")}, FallbackLocales: []string{"DE-de", "en"}})
    })
}

func TestNewManager_TheRefusalsNameTheConfigField(t *testing.T) {
    expectRefusalContext(t, "field", "ManagerConfig.FallbackLocales", func() {
        NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("en")}, FallbackLocales: []string{"", "en"}})
    })

    expectRefusalContext(t, "field", "ManagerConfig.FallbackLocales", func() {
        NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{NewMapCatalog("en")}, FallbackLocales: []string{"de", "en"}})
    })
}

/* a chain with catalogs and no fallback asks only the locale and its base, so a request in a locale no catalog carries would answer raw ids; the manager refuses it and the application ends the chain on its default locale */
func TestNewManager_RefusesCatalogsWithNoFallbackLocale(t *testing.T) {
    english := NewMapCatalog("en")

    for _, fallbackLocales := range [][]string{nil, {}} {
        refusal := recoveredManagerRefusal(func() {
            NewManager(ManagerConfig{Catalogs: []translationcontract.Catalog{english}, FallbackLocales: fallbackLocales})
        })
        if nil == refusal || "translation fallback locales are empty while catalogs are handed" != refusal.Message() || "ManagerConfig.FallbackLocales" != refusal.Context()["field"] {
            t.Fatalf("%v: expected the empty fallback list refused naming the field, got %v", fallbackLocales, refusal)
        }
    }

    if refusal := recoveredManagerRefusal(func() { NewManager(ManagerConfig{}) }); nil != refusal {
        t.Fatalf("expected a manager with no catalog admitted without fallbacks, got %v", refusal)
    }
}

func recoveredManagerRefusal(callback func()) (refusal *exception.Error) {
    defer func() {
        if recovered, isError := recover().(*exception.Error); true == isError {
            refusal = recovered
        }
    }()

    callback()

    return nil
}
