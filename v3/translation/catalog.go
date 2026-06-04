package translation

import (
    translationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

const DefaultDomain = "messages"

func NewMapCatalog(locale string) *MapCatalog {
    return &MapCatalog{
        locale:           locale,
        messagesByDomain: make(map[string]map[string]string),
    }
}

type MapCatalog struct {
    locale           string
    messagesByDomain map[string]map[string]string
}

func (instance *MapCatalog) Locale() string {
    return instance.locale
}

func (instance *MapCatalog) Add(domain string, messageId string, message string) *MapCatalog {
    if "" == domain {
        domain = DefaultDomain
    }

    messages, exists := instance.messagesByDomain[domain]
    if false == exists {
        messages = make(map[string]string)
        instance.messagesByDomain[domain] = messages
    }

    messages[messageId] = message

    return instance
}

func (instance *MapCatalog) Get(messageId string, domain string) (string, bool) {
    if "" == domain {
        domain = DefaultDomain
    }

    messages, exists := instance.messagesByDomain[domain]
    if false == exists {
        return "", false
    }

    message, found := messages[messageId]
    return message, found
}

var _ translationcontract.Catalog = (*MapCatalog)(nil)
