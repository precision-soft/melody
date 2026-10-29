package contract

type Translator interface {
    Trans(messageId string, parameters map[string]any, domain string, locale string) string

    HasMessage(messageId string, domain string, locale string) bool
}
