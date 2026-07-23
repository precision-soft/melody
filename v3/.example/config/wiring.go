package config

import (
    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

const (
    wiringImportPathPrefix = "github.com/precision-soft/melody/v3/.example/"
)

/* NewWiringBindSet declares which packages melody:wiring:generate scans and how the scalar arguments of the constructors it finds are filled. Everything else a constructor asks for is resolved from the container by type, so only the values that cannot come from there are named here.

The repository and service packages need no binds at all: every argument of their constructors is a service. They are scanned so that adding one is a matter of writing the constructor and regenerating, instead of also remembering to register it. */
func NewWiringBindSet() *melodywiring.BindSet {
    bindSet := melodywiring.NewBindSet()

    bindSet.Package(wiringImportPathPrefix+"repository", "repository")

    bindSet.Package(wiringImportPathPrefix+"service", "service")

    bindSet.Package(wiringImportPathPrefix+"reporting", "reporting").
        Name("catalogTitle", "app.catalog_title").
        Name("maxItemsPerPage", "app.max_items_per_page")

    return bindSet
}
