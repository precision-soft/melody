package config

import (
    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

const (
    wiringImportPathPrefix = "github.com/precision-soft/melody/v3/.example/"
)

/* NewWiringBindSet declares which packages melody:wiring:generate scans and how the scalar arguments of the constructors it finds are filled. Everything else a constructor asks for is resolved from the container by type, so only the values that cannot come from there are named here.

   The repository package needs no binds at all: every argument of its constructors is a service. The service package needs one, for the endpoint of the rate provider, and it is a PARAMETER rather than the .env key of the same value because a bound argument is read with MustGet: an auto-registered key disappears with its line in .env and takes the boot with it, while the parameter is declared with an empty-string fallback and answers "" — which is what "this door is unwired" means everywhere else here. Both packages are scanned so that adding a service is a matter of writing the constructor and regenerating, instead of also remembering to register it. */
func NewWiringBindSet() *melodywiring.BindSet {
    bindSet := melodywiring.NewBindSet()

    bindSet.Package(wiringImportPathPrefix+"repository", "repository")

    bindSet.Package(wiringImportPathPrefix+"service", "service").
        Name("ratesBaseUrl", parameterRatesBaseUrl)

    bindSet.Package(wiringImportPathPrefix+"reporting", "reporting").
        Name("catalogTitle", "app.catalog_title").
        Name("maxItemsPerPage", "app.max_items_per_page").
        Name("exportEndpoint", parameterReportExportEndpoint)

    return bindSet
}
