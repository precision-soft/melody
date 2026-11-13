package translation

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

/* NewManager keeps every catalog of a locale, in the order given, and a lookup asks them in that order until one answers, since a locale is assembled from several sources; nil is a manager with no catalog, which answers raw ids. A nil catalog, or one whose Locale is empty and so could never be found, is refused; once a catalog is handed, so is a default or fallback locale that is empty or that no catalog carries, since the lookup asks them by that exact name. */
func NewManager(
    catalogs []translationcontract.Catalog,
    defaultLocale string,
    fallbackLocales []string,
) *Manager {
    catalogsByLocale := make(map[string][]translationcontract.Catalog)
    for _, catalog := range catalogs {
        /* a nil catalog is refused, since skipping it would build a translator answering raw ids for a whole locale */
        if true == internal.IsNilInterface(catalog) {
            exception.Panic(exception.NewError("translation catalog is nil", nil, nil))
        }

        locale := catalog.Locale()
        if "" == locale {
            exception.Panic(exception.NewError("translation catalog carries no locale", nil, nil))
        }

        catalogsByLocale[locale] = append(catalogsByLocale[locale], catalog)
    }

    if 0 < len(catalogsByLocale) {
        if "" == defaultLocale {
            exception.Panic(exception.NewError("translation default locale is empty", nil, nil))
        }

        if 0 == len(catalogsByLocale[defaultLocale]) {
            exception.Panic(
                exception.NewError(
                    "translation default locale is carried by no catalog",
                    exceptioncontract.Context{"defaultLocale": defaultLocale},
                    nil,
                ),
            )
        }

        for index, fallbackLocale := range fallbackLocales {
            if "" == fallbackLocale {
                exception.Panic(exception.NewError("translation fallback locale is empty", exceptioncontract.Context{"index": index}, nil))
            }

            if 0 == len(catalogsByLocale[fallbackLocale]) {
                exception.Panic(
                    exception.NewError(
                        "translation fallback locale is carried by no catalog",
                        exceptioncontract.Context{"fallbackLocale": fallbackLocale, "index": index},
                        nil,
                    ),
                )
            }
        }
    }

    return &Manager{
        defaultLocale:    defaultLocale,
        fallbackLocales:  append([]string{}, fallbackLocales...),
        catalogsByLocale: catalogsByLocale,
    }
}

type Manager struct {
    defaultLocale    string
    fallbackLocales  []string
    catalogsByLocale map[string][]translationcontract.Catalog
}

func (instance *Manager) Trans(
    messageId string,
    parameters map[string]any,
    domain string,
    locale string,
) string {
    pattern, resolvedLocale, found := instance.lookup(messageId, domain, locale)
    if false == found {
        return messageId
    }

    return formatMessage(pattern, parameters, resolvedLocale)
}

func (instance *Manager) HasMessage(messageId string, domain string, locale string) bool {
    _, _, found := instance.lookup(messageId, domain, locale)
    return found
}

func (instance *Manager) lookup(messageId string, domain string, locale string) (string, string, bool) {
    /* the empty domain resolves here, the door every catalog is asked through, since the contract does not oblige an application's catalog to coerce it */
    if "" == domain {
        domain = DefaultDomain
    }

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
    chain := make([]string, 0, 3+len(instance.fallbackLocales))
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

    appendLocale(locale)
    appendLocale(baseLocale(locale))

    for _, fallback := range instance.fallbackLocales {
        appendLocale(fallback)
    }

    appendLocale(instance.defaultLocale)

    return chain
}

var _ translationcontract.Translator = (*Manager)(nil)
