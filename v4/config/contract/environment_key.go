package contract

/* EnvironmentKey is one .env key a module owns. Key and Parameter name one parameter, as MELODY_HTTP_ADDRESS and kernel.http_address do; Field is what a refusal names beside the key. Default is the text the key takes when no .env file sets it, written as the .env line would be and possibly a template; the empty text is absent. Rule is nil for free text. */
type EnvironmentKey struct {
    Key       string
    Parameter string
    Field     string
    Default   string
    Secret    bool
    Rule      EnvironmentKeyRule
}

/* EnvironmentKeyRule judges the resolved text of a key; an error names field. Describe is the rule in words, for the documentation of a module. */
type EnvironmentKeyRule interface {
    Check(text string, field string) error

    Describe() string
}

/* EnvironmentKeyDeclarer is the part of a configuration that can hold the keys a module declares; Configuration does not demand it, and the application refuses a module that declares keys over a configuration without it. */
type EnvironmentKeyDeclarer interface {
    DeclareEnvironmentKey(owner string, key EnvironmentKey) error

    CheckEnvironmentKeys() error

    UndeclaredEnvironmentKeys() []string
}
