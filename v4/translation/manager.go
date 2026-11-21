package translation

import (
    "errors"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

/* ErrMessageNotFound marks a message no catalog of the locale chain carries; Translate answers it beside the message id. */
var ErrMessageNotFound = errors.New("translation message not found")

/* ManagerConfig is what a manager is built over: the catalogs, nil for none, which answers raw ids, and the fallback locales a lookup asks, in order, after the locale asked and its base; nothing is asked after the last, so an application ends the list on its default locale. */
type ManagerConfig struct {
    Catalogs        []translationcontract.Catalog
    FallbackLocales []string
}

/* NewManager keeps every catalog of a locale, in the order given, and a lookup asks them in that order until one answers, since a locale is assembled from several sources. Every locale, a catalog's, a fallback and the one a lookup asks, is matched in BCP 47 case, so ro-ro finds the catalog of ro-RO; two catalogs spelling one locale differently are refused, naming both spellings. A nil catalog, or one whose Locale is empty and so could never be found, is refused; once a catalog is handed, so is a fallback locale that is empty or that no catalog carries. */
func NewManager(config ManagerConfig) *Manager {
    catalogs := config.Catalogs
    fallbackLocales := config.FallbackLocales

    catalogsByLocale := make(map[string][]translationcontract.Catalog)
    spellingByLocale := make(map[string]string)
    for _, catalog := range catalogs {
        /* a nil catalog is refused, since skipping it would build a translator answering raw ids for a whole locale */
        if true == internal.IsNilInterface(catalog) {
            exception.Panic(exception.NewError("translation catalog is nil", nil, nil))
        }

        locale := catalog.Locale()
        if "" == locale {
            exception.Panic(exception.NewError("translation catalog carries no locale", nil, nil))
        }

        canonical := canonicalLocale(locale)
        if spelling, registered := spellingByLocale[canonical]; true == registered && spelling != locale {
            exception.Panic(
                exception.NewError(
                    "translation catalogs spell one locale two ways",
                    exceptioncontract.Context{"locale": spelling, "otherSpelling": locale},
                    nil,
                ),
            )
        }

        spellingByLocale[canonical] = locale
        catalogsByLocale[canonical] = append(catalogsByLocale[canonical], catalog)
    }

    canonicalFallbacks := make([]string, 0, len(fallbackLocales))
    for _, fallbackLocale := range fallbackLocales {
        canonicalFallbacks = append(canonicalFallbacks, canonicalLocale(fallbackLocale))
    }

    if 0 < len(catalogsByLocale) {
        for index, fallbackLocale := range fallbackLocales {
            if "" == fallbackLocale {
                exception.Panic(exception.NewError("translation fallback locale is empty", exceptioncontract.Context{"index": index, "field": "ManagerConfig.FallbackLocales"}, nil))
            }

            if 0 == len(catalogsByLocale[canonicalFallbacks[index]]) {
                exception.Panic(
                    exception.NewError(
                        "translation fallback locale is carried by no catalog",
                        exceptioncontract.Context{"fallbackLocale": fallbackLocale, "index": index, "field": "ManagerConfig.FallbackLocales"},
                        nil,
                    ),
                )
            }
        }
    }

    return &Manager{
        fallbackLocales:  canonicalFallbacks,
        catalogsByLocale: catalogsByLocale,
    }
}

type Manager struct {
    fallbackLocales  []string
    catalogsByLocale map[string][]translationcontract.Catalog
}

func (instance *Manager) Translate(messageId string, config translationcontract.TranslateConfig) (string, error) {
    pattern, resolvedLocale, found := instance.lookup(messageId, config.Domain, config.Locale)
    if false == found {
        return messageId, exception.NewError(
            "translation message not found",
            exceptioncontract.Context{"messageId": messageId, "domain": domainOrDefault(config.Domain), "locale": config.Locale},
            ErrMessageNotFound,
        )
    }

    /* a catalog of the application's own may hold a pattern the renderer cannot render whole, so it is judged before it is rendered */
    if true == strings.ContainsRune(pattern, '{') {
        if defect := findPatternDefect([]rune(pattern), 0); nil != defect {
            return "", exception.NewError(
                "translation message pattern is malformed",
                malformedPatternContext(resolvedLocale, domainOrDefault(config.Domain), messageId, defect),
                nil,
            )
        }
    }

    return formatMessage(pattern, config.Parameters, resolvedLocale), nil
}

func (instance *Manager) HasMessage(messageId string, config translationcontract.TranslateConfig) bool {
    _, _, found := instance.lookup(messageId, config.Domain, config.Locale)
    return found
}

func (instance *Manager) lookup(messageId string, domain string, locale string) (string, string, bool) {
    /* the empty domain resolves here, the door every catalog is asked through, since the contract does not oblige an application's catalog to coerce it */
    domain = domainOrDefault(domain)

    for _, candidate := range instance.localeChain(locale) {
        for _, catalog := range instance.catalogsByLocale[candidate] {
            message, found := catalog.Get(messageId, domain)
            if true == found {
                return message, candidate, true
            }
        }
    }

    return "", "", false
}

func (instance *Manager) localeChain(locale string) []string {
    chain := make([]string, 0, 2+len(instance.fallbackLocales))
    seen := make(map[string]bool)

    appendLocale := func(value string) {
        if "" == value {
            return
        }

        if true == seen[value] {
            return
        }

        seen[value] = true
        chain = append(chain, value)
    }

    locale = canonicalLocale(locale)
    appendLocale(locale)
    appendLocale(baseLocale(locale))

    for _, fallback := range instance.fallbackLocales {
        appendLocale(fallback)
    }

    return chain
}

func domainOrDefault(domain string) string {
    if "" == domain {
        return DefaultDomain
    }

    return domain
}

var _ translationcontract.Translator = (*Manager)(nil)
