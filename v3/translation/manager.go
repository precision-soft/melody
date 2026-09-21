package translation

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    translationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

/* NewManager keeps every catalog of a locale, in the order given, and a lookup asks them in that order until one answers: a locale is assembled from several sources — the json loader builds one catalog per file it reads — so a second catalog of a locale is an ordinary wiring, and keying the locale on one catalog made the second silently replace the first, every message that lived only in the first answering its raw id. A catalog whose Locale is empty is refused as the nil one is: the locale chain never asks for the empty locale, so such a catalog could never be found. */
func NewManager(
    defaultLocale string,
    fallbackLocales []string,
    catalogs ...translationcontract.Catalog,
) *Manager {
    catalogsByLocale := make(map[string][]translationcontract.Catalog)
    for _, catalog := range catalogs {
        /* refused, not skipped: a nil catalog is a wiring mistake, and dropping it silently builds a translator that answers raw message ids for a whole locale with nothing pointing at the hole — the same judgement every sibling constructor applies to a nil collaborator */
        if true == internal.IsNilInterface(catalog) {
            exception.Panic(exception.NewError("translation catalog is nil", nil, nil))
        }

        locale := catalog.Locale()
        if "" == locale {
            exception.Panic(exception.NewError("translation catalog carries no locale", nil, nil))
        }

        catalogsByLocale[locale] = append(catalogsByLocale[locale], catalog)
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
    /* the empty domain resolves HERE, at the one door every catalog is asked through: the shipped MapCatalog coerces it too, but the contract does not oblige an application's catalog to, and a lookup handing "" through verbatim missed in exactly the catalogs that took the contract at its word */
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
