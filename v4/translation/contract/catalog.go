package contract

/* Catalog serves the patterns Trans renders; a catalog of its own validates what it holds with translation.ValidatePattern, as MapCatalog and the json loader do, since Trans renders a malformed select or plural without an error, often as an empty string. */
type Catalog interface {
    Locale() string

    Get(messageId string, domain string) (string, bool)
}

type Loader interface {
    Load() ([]Catalog, error)
}
