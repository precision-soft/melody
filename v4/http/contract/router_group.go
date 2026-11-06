package contract

/* GroupConfig is what a route group prefixes its routes with. A field left out adds nothing: no path prefix, no name prefix, no requirement, no default. */
type GroupConfig struct {
    /* PathPrefix is joined before every pattern, as JoinPaths joins it. */
    PathPrefix string

    /* NamePrefix is written before every named route's name; an unnamed route stays unnamed. */
    NamePrefix string

    /* Requirements are added to every route that does not name the key itself; the map is copied. */
    Requirements map[string]string

    /* Defaults are added to every route that does not name the key itself; the map is copied. */
    Defaults map[string]string
}

/* RouteGroup registers routes under one prefix; it is built whole by Router.Group and takes no setting after. */
type RouteGroup interface {
    RouteHandler
}
