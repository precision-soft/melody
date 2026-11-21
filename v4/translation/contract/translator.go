package contract

/* Translator renders a message of its catalogs: Translate answers the rendered message and nil; for a message no catalog carries, the message id and an error matching translation.ErrMessageNotFound; for a pattern that cannot be rendered whole, an empty string and the error naming the defect. HasMessage reads the domain and the locale of the config, never its parameters. */
type Translator interface {
    Translate(messageId string, config TranslateConfig) (string, error)

    HasMessage(messageId string, config TranslateConfig) bool
}

/* TranslateConfig is the zero value for no parameter, the default domain and the translator's default locale chain. */
type TranslateConfig struct {
    Parameters map[string]any
    Domain     string
    Locale     string
}
