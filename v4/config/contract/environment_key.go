package contract

/* EnvironmentKey is one .env key a module or the application owns: a module's is MELODY_ followed by upper-case letters, digits and underscores, the application's any such name outside MELODY_. Key and Parameter name one parameter, as MELODY_HTTP_ADDRESS and kernel.http_address do; Field is what a refusal names beside the key. Default is the text the key takes when no .env file sets it, written as the .env line would be and possibly a template; the empty text is absent. Rule is nil for free text. */
type EnvironmentKey struct {
    Key       string
    Parameter string
    Field     string
    Default   string
    Secret    bool
    Rule      EnvironmentKeyRule

    /* Retired are the spellings this key replaced: a .env artifact that still sets one refuses the boot naming this key, whatever its value. A spelling of the wrong shape (a module's outside the MELODY_ namespace, the application's not upper-case letters, digits and underscores), equal to Key or to Parameter, listed twice, or held or retired by the framework or another declaration refuses the declaration. */
    Retired []string
}

/* RetiredEnvironmentKey is one retired spelling the .env artifacts still set, with the key that replaced it and the owner that retired it. */
type RetiredEnvironmentKey struct {
    Key       string
    Successor string
    Owner     string
}

/* EnvironmentKeyRule judges the resolved text of a key; an error names field. Describe is the rule in words, for the documentation of a module. */
type EnvironmentKeyRule interface {
    Check(text string, field string) error

    Describe() string
}

/* EnvironmentKeyDeclarer is the part of a configuration that can hold the keys a module or the application declares; Configuration does not demand it, and the application refuses a module that declares keys over a configuration without it. */
type EnvironmentKeyDeclarer interface {
    DeclareEnvironmentKey(owner string, key EnvironmentKey) error

    CheckEnvironmentKeys() error

    UndeclaredEnvironmentKeys() []string

    /* RetiredEnvironmentKeys answers, sorted by key, every retired spelling the .env artifacts set, the framework's and the declared ones. */
    RetiredEnvironmentKeys() []RetiredEnvironmentKey

    /* RetiredEnvironmentKeySuccessor answers the key that replaced a retired spelling, the framework's or a declared one, whether or not an artifact sets it. */
    RetiredEnvironmentKeySuccessor(key string) (string, bool)
}
