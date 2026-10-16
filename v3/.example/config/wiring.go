package config

import (
    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

const (
    wiringImportPathPrefix = "github.com/precision-soft/melody/v3/.example/"
)

/* NewWiringBindSet declares which packages melody:wiring:generate scans and binds the constructor scalars that cannot come from the container, which resolves every other argument by type. The service package binds the rate provider's endpoint and the catalogue's base currency to parameters rather than .env keys, because a bound argument is read with MustGet and a parameter declared with an empty-string fallback survives its .env line being removed. */
func NewWiringBindSet() *melodywiring.BindSet {
    bindSet := melodywiring.NewBindSet()

    bindSet.Package(wiringImportPathPrefix+"repository", "repository")

    bindSet.Package(wiringImportPathPrefix+"service", "service").
        Name("ratesBaseUrl", parameterRatesBaseUrl).
        Name("ratesBaseCurrency", parameterRatesBaseCurrency)

    bindSet.Package(wiringImportPathPrefix+"reporting", "reporting").
        Name("catalogTitle", "app.catalog_title").
        Name("maxItemsPerPage", "app.max_items_per_page").
        Name("exportEndpoint", parameterReportExportEndpoint)

    return bindSet
}
