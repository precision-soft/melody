package contract

import (
    "net/url"
)

type UrlGenerator interface {
    /* GeneratePath answers the path of a named route, its query and its fragment appended when the config carries them: a relative reference, never behind the public base's path. */
    GeneratePath(routeName string, config PathConfig) (string, error)

    /* GenerateUrl answers the same reference behind the application's public base, MELODY_HTTP_PUBLIC_URL, its path joined before the route's with one slash; it is an error when no base is set. A request's Host is never read. */
    GenerateUrl(routeName string, config PathConfig) (string, error)
}

/* PathConfig is the zero value for a route with no placeholder, no query and no fragment; a query key that is empty is skipped, the keys written sorted, a repeated key's values in their order. */
type PathConfig struct {
    Parameters map[string]string
    Query      url.Values
    Fragment   string
}
