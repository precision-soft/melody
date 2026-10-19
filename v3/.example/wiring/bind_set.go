package wiring

import (
    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

const (
    importPathPrefix = "github.com/precision-soft/melody/v3/.example/"
)

/* the parameters the bind set binds constructor scalars to; config declares them, and both read the names from here */
const (
    ParameterRatesBaseUrl         = "app.rates.base_url"
    ParameterReportExportEndpoint = "app.reporting.export_endpoint"
    ParameterRatesBaseCurrency    = "app.rates.base_currency"
)

/* NewBindSet declares which packages melody:wiring:generate scans and binds the constructor scalars that cannot come from the container, which resolves every other argument by type. The service package binds the rate provider's endpoint and the catalogue's base currency to parameters rather than .env keys, because a bound argument is read with MustGet and a parameter declared with an empty-string fallback survives its .env line being removed. It lives outside config, which imports the generated package, so the generator under wiring/generate compiles on a tree that does not hold the generated file yet. */
func NewBindSet() *melodywiring.BindSet {
    bindSet := melodywiring.NewBindSet()

    bindSet.Package(importPathPrefix+"repository", "repository")

    bindSet.Package(importPathPrefix+"service", "service").
        Name("ratesBaseUrl", ParameterRatesBaseUrl).
        Name("ratesBaseCurrency", ParameterRatesBaseCurrency)

    bindSet.Package(importPathPrefix+"reporting", "reporting").
        Name("catalogTitle", "app.catalog_title").
        Name("maxItemsPerPage", "app.max_items_per_page").
        Name("exportEndpoint", ParameterReportExportEndpoint)

    return bindSet
}
