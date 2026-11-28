package translation

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
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

    if defect := findPatternDefect([]rune(message), 0); nil != defect {
        exception.Panic(
            exception.NewError(
                "translation message pattern is malformed",
                malformedPatternContext(instance.locale, domain, messageId, defect),
                nil,
            ),
        )
    }

    messages, exists := instance.messagesByDomain[domain]
    if false == exists {
        messages = make(map[string]string)
        instance.messagesByDomain[domain] = messages
    }

    messages[messageId] = message

    return instance
}

func malformedPatternContext(locale string, domain string, messageId string, defect *patternDefect) exceptioncontract.Context {
    return exceptioncontract.Context{
        "locale":    locale,
        "domain":    domain,
        "messageId": messageId,
        "offset":    defect.offset,
        "reason":    defect.reason,
    }
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
