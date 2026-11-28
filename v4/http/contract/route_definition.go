package contract

import (
    "reflect"
)

type RouteDefinition interface {
    Name() string

    Pattern() string

    Methods() []string

    Host() string

    Schemes() []string

    Requirements() map[string]string

    Defaults() map[string]string

    Locales() []string

    Priority() int

    Attributes() map[string]any

    /* RequestType is the body type the route declared through RouteOptions.RequestType, nil when it declared none. */
    RequestType() reflect.Type
}
