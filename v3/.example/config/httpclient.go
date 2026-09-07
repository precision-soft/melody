package config

import (
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/httpclient"
)

/* The two outbound clients this application holds. They are CONTAINER services rather than values their
   callers build per call for the reason the package's own documentation gives: every client owns a
   transport with an idle connection pool — a hundred connections per host, kept for ninety seconds — so one
   built inside the called function leaks a pool per call. Registered here, the container closes each at
   teardown through the Close() error it declares, with no shutdown hook of this application's own.

   They are two clients and not one because they point at two services, which is the same rule. It also
   makes them the two SHAPES the client supports, and the shapes are not interchangeable: a client with a
   base url refuses a target that resolves off that origin, so it cannot be handed an endpoint an operator
   configured; a client without one refuses a relative target, because the request it would build could name
   no host at all. The names live beside their consumers, in the service package, the way the server-sent
   event hub's does. */

/* outboundRequestTimeout bounds one exchange with an outbound provider. It is deliberately far below the
   ceilings the client would otherwise apply, and those were measured rather than assumed: a per-request
   budget that is not positive falls back to the CLIENT's timeout, and a client built without one is clamped
   to thirty seconds by the constructor — while against a provider that accepts the connection and never
   sends headers the transport's own ResponseHeaderTimeout (fifteen seconds by default) is what actually
   ends the wait. The buffered path therefore has no unbounded spelling at all, and this budget can only
   ever NARROW what the client would have allowed. Both callers run on a schedule with nothing waiting on
   them, so the number is chosen to fail fast and let the caller's retry decide, not to give a slow provider
   every chance. */
const outboundRequestTimeout = 3 * time.Second

/* registerRatesHttpClientService follows the convention every optional integration in this application
   follows: the env key names the provider, and an absent key leaves the door unwired rather than failing
   the boot. Nothing resolves this service unless a refresh actually runs, so an http process that never
   refreshes never builds a client and never opens a pool. */
func (instance *Module) registerRatesHttpClientService(registrar melodyapplicationcontract.ServiceRegistrar) {
    baseUrl := instance.environmentValue(parameterRatesBaseUrl)
    if "" == baseUrl {
        return
    }

    registrar.RegisterService(
        service.ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            /* the base url carries a path and therefore its trailing slash, and every target the refresh
               names is RELATIVE to it. That is not a style choice: the client resolves a target against the
               base by RFC 3986, so an absolute-path target ("/latest") would replace the base path entirely
               and reach a resource one segment up — measured against the running provider, 404 where the
               relative spelling answers 200. A base whose path lacks the slash is refused at construction,
               which is where the wiring mistake is made. */
            return httpclient.NewHttpClient(
                httpclient.NewHttpClientConfig(
                    baseUrl,
                    outboundRequestTimeout,
                    map[string]string{
                        "accept": "application/json",
                    },
                ),
            ), nil
        },
        outboundClientRegisterOptions()...,
    )
}

/* registerReportExportHttpClientService builds the other shape. The export endpoint is a whole url an
   operator configures, host included, so this client carries NO base: a based client judges the resolved
   url against its own origin and would refuse the endpoint the moment it named a different host, which is
   exactly the configuration an operator is entitled to write. The caller therefore hands it the absolute
   url, the only spelling a baseless client accepts. */
func (instance *Module) registerReportExportHttpClientService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if "" == instance.environmentValue(parameterReportExportEndpoint) {
        return
    }

    registrar.RegisterService(
        service.ServiceReportExportHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return httpclient.NewHttpClient(
                httpclient.NewHttpClientConfig(
                    "",
                    outboundRequestTimeout,
                    map[string]string{
                        "accept": "application/json",
                    },
                ),
            ), nil
        },
        outboundClientRegisterOptions()...,
    )
}

/* outboundClientRegisterOptions keeps both clients OFF the by-type index, and neither of them is the one
   that "loses" it: they are the same concrete type, so a resolution by type could only answer with whichever
   registration happened to land first, and a caller asking for "the http client" of an application that
   holds two would be answered arbitrarily. Registered by name only, the ambiguity cannot be spelled — asking
   for a client means naming which provider it points at. Measured before it was written: with both on the
   index the boot refuses with a serviceType collision on the second, which is the framework saying the same
   thing. */
func outboundClientRegisterOptions() []melodycontainercontract.RegisterOption {
    return []melodycontainercontract.RegisterOption{
        melodycontainer.WithoutTypeRegistration(),
    }
}
