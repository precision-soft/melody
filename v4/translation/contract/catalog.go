package contract

import (
    "context"
)

/* Catalog serves the patterns Translate renders; a catalog of its own validates what it holds with translation.ValidatePattern, as MapCatalog and the json loader do, so a malformed select or plural is refused where it is added rather than answered as Translate's error at each call. */
type Catalog interface {
    Locale() string

    Get(messageId string, domain string) (string, bool)
}

type Loader interface {
    Load(ctx context.Context) ([]Catalog, error)
}
