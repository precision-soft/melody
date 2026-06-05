package translation

import (
    "github.com/precision-soft/melody/v3/internal"
    translationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

func NewManager(
    defaultLocale string,
    fallbackLocales []string,
    catalogs ...translationcontract.Catalog,
) *Manager {
    catalogsByLocale := make(map[string]translationcontract.Catalog)
    for _, catalog := range catalogs {
        if true == internal.IsNilInterface(catalog) {
            continue
        }

        catalogsByLocale[catalog.Locale()] = catalog
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
    catalogsByLocale map[string]translationcontract.Catalog
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
    for _, candidate := range instance.localeChain(locale) {
        catalog, exists := instance.catalogsByLocale[candidate]
        if false == exists {
            continue
        }

        message, found := catalog.Get(messageId, domain)
        if true == found {
            return message, candidate, true
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
