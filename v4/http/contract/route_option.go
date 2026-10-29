package contract

/* RouteOptions is what a route declares beyond its pattern and its handler. The zero value registers the route under no name, for every method, host and scheme, with no requirement, default, locale or attribute, at priority zero; the lists and the maps are copied at registration, so the value can be reused. */
type RouteOptions struct {
    /* Name is the route's name for the url generator and the manifest; "" leaves the route unnamed. */
    Name string

    /* Methods are the http methods the route answers; nil or empty answers every method. */
    Methods []string

    /* Host is the host the route is bound to; "" binds it to every host. */
    Host string

    /* Schemes are the schemes the route answers; nil or empty answers every scheme. */
    Schemes []string

    /* Requirements are the regular expressions a parameter must match whole, keyed by parameter name; an empty key or an empty pattern is skipped, an invalid pattern refused at registration. */
    Requirements map[string]string

    /* Defaults are the parameter values a generated url takes when the caller gives none. */
    Defaults map[string]string

    /* Locales is the whitelist the _locale parameter is checked against; nil or empty admits every locale. */
    Locales []string

    /* Priority decides among routes that all match a request, the higher first; equal priorities keep registration order. */
    Priority int

    /* Attributes travel with the match into the request; the keys the framework reserves are refused at registration. */
    Attributes map[string]any
}
