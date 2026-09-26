package config

import (
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/httpclient"
)

/* The two outbound clients are container services, because each owns a transport with an idle connection pool and the container closes each at teardown through its Close. There are two because they point at two services, and they are the client's two shapes: a based client refuses a target off its origin, a baseless one refuses a relative target. */

/* outboundRequestTimeout bounds one exchange with an outbound provider. A non-positive per-request budget falls back to the client's timeout, which the constructor clamps to thirty seconds, so this value can only narrow what the client allows; both callers run on a schedule, so it fails fast and leaves the decision to the caller's retry. */
const outboundRequestTimeout = 3 * time.Second

/* registerRatesHttpClientService leaves the client unwired when the provider key is absent; nothing resolves it unless a refresh runs, so an http process opens no pool for it. */
func (instance *Module) registerRatesHttpClientService(registrar melodyapplicationcontract.ServiceRegistrar) {
    baseUrl := instance.environmentValue(parameterRatesBaseUrl)
    if "" == baseUrl {
        return
    }

    registrar.RegisterService(
        service.ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            /* every target the refresh names is relative to the base url, which carries its trailing slash: the client resolves a target by RFC 3986, so an absolute-path target would replace the base path. A base whose path lacks the slash is refused at construction. */
            return httpclient.NewHttpClient(ratesHttpClientConfig(baseUrl)), nil
        },
        outboundClientRegisterOptions()...,
    )
}

/* registerReportExportHttpClientService builds the baseless shape, because the export endpoint is a whole url an operator configures and a based client would refuse it once it named another host. It follows no redirect: net/http re-sends a POST answered 301, 302 or 303 as a GET without its body, so the exporter sees the 3xx and refuses it rather than read a moved sink's 200 as a delivery. */
func (instance *Module) registerReportExportHttpClientService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if "" == instance.environmentValue(parameterReportExportEndpoint) {
        return
    }

    registrar.RegisterService(
        service.ServiceReportExportHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return httpclient.NewHttpClient(reportExportHttpClientConfig()), nil
        },
        outboundClientRegisterOptions()...,
    )
}

/* ratesHttpClientConfig is the rates client: based on the provider's url, bounded by the outbound budget,
   asking for json. */
func ratesHttpClientConfig(baseUrl string) *httpclient.HttpClientConfig {
    return httpclient.NewHttpClientConfig(
        baseUrl,
        outboundRequestTimeout,
        map[string]string{
            "accept": "application/json",
        },
    )
}

/* reportExportHttpClientConfig is the export client: no base, the same budget and accept, and no redirect
   followed. */
func reportExportHttpClientConfig() *httpclient.HttpClientConfig {
    return httpclient.NewHttpClientConfig(
        "",
        outboundRequestTimeout,
        map[string]string{
            "accept": "application/json",
        },
    ).WithoutRedirects()
}

/* outboundClientRegisterOptions keeps both clients off the by-type index: they share a concrete type, so a resolution by type would answer arbitrarily, and a caller names the provider its client points at. */
func outboundClientRegisterOptions() []melodycontainercontract.RegisterOption {
    return []melodycontainercontract.RegisterOption{
        melodycontainer.WithoutTypeRegistration(),
    }
}
