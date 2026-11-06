package contract

type Catalog interface {
    Locale() string

    Get(messageId string, domain string) (string, bool)
}

type Loader interface {
    Load() ([]Catalog, error)
}
