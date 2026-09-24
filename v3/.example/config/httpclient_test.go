package config

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/httpclient"
)

/* the export client is the one the exporter posts a reading through, and a redirect followed there turns the POST into a GET without its body: the 200 of whatever page the sink pointed at read as the sink having received the reading. The client the composition root registers hands the 3xx back instead, so the exporter can refuse it by name. */
func TestRegisterReportExportHttpClientService_TheClientDoesNotFollowARedirect(t *testing.T) {
    sinkPosts := 0
    pageGets := 0

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        if "/sink" == request.URL.Path {
            sinkPosts = sinkPosts + 1
            nethttp.Redirect(writer, request, "/page", nethttp.StatusFound)

            return
        }

        pageGets = pageGets + 1
        writer.WriteHeader(nethttp.StatusOK)
    }))
    t.Cleanup(server.Close)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    moduleWithEnvironment(t, map[string]string{parameterReportExportEndpoint: server.URL + "/sink"}).
        registerReportExportHttpClientService(containerRegistrar{Container: containerInstance})

    client, resolveErr := melodycontainer.FromResolver[*httpclient.HttpClient](containerInstance, service.ServiceReportExportHttpClient)
    if nil != resolveErr {
        t.Fatalf("resolve the export client: %v", resolveErr)
    }

    response, requestErr := client.Post(server.URL+"/sink", map[string]any{"reading": 1})
    if nil != requestErr {
        t.Fatalf("expected the redirect to be answered rather than followed: %v", requestErr)
    }

    if nethttp.StatusFound != response.StatusCode() || 1 != sinkPosts || 0 != pageGets {
        t.Fatalf("expected the 302 back after one post and no read of the page, got %d after %d posts and %d reads", response.StatusCode(), sinkPosts, pageGets)
    }
}

/* the rates client is based on the provider's url, bounded by the outbound budget — three seconds, which only
   ever narrows what the client would allow — asks for json and follows no one else's redirects policy but the
   client's own; the export client carries no base, since the endpoint an operator writes names its own host,
   the same budget and accept, and follows no redirect */
func TestOutboundHttpClientConfigs_CarryTheirBaseTheBudgetAndTheAcceptHeader(t *testing.T) {
    rates := ratesHttpClientConfig("https://rates.example/v1/")
    if "https://rates.example/v1/" != rates.BaseUrl() || 3*time.Second != rates.Timeout() || "application/json" != rates.Headers()["Accept"] || 1 != len(rates.Headers()) {
        t.Fatalf("expected the rates client based on the provider, 3s, accept json, got %q %s %v", rates.BaseUrl(), rates.Timeout(), rates.Headers())
    }

    if false == rates.FollowsRedirects() {
        t.Fatal("expected the rates client to keep the client's redirect policy")
    }

    export := reportExportHttpClientConfig()
    if "" != export.BaseUrl() || 3*time.Second != export.Timeout() || "application/json" != export.Headers()["Accept"] || 1 != len(export.Headers()) {
        t.Fatalf("expected the export client without a base, 3s, accept json, got %q %s %v", export.BaseUrl(), export.Timeout(), export.Headers())
    }

    if true == export.FollowsRedirects() {
        t.Fatal("expected the export client to follow no redirect")
    }
}

/* the rates client is registered when the parameter names a provider and reaches it: the relative target the
   refresh names resolves under the base, the accept header travels. The fixture's environment key is the
   parameter's own name, the one environmentValue reads. Without a provider nothing is registered, and an http
   process that never refreshes opens no pool. Neither client is on the by-type index: both are one concrete
   type, and "the http client" of an application that holds two has no answer. */
func TestRegisterRatesHttpClientService_ReachesTheProviderUnderItsBaseAndStaysOffTheTypeIndex(t *testing.T) {
    requestedPath := ""
    acceptHeader := ""

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        requestedPath = request.URL.Path
        acceptHeader = request.Header.Get("accept")
        writer.WriteHeader(nethttp.StatusOK)
    }))
    t.Cleanup(server.Close)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    moduleInstance := moduleWithEnvironment(t, map[string]string{
        parameterRatesBaseUrl:         server.URL + "/v1/",
        parameterReportExportEndpoint: server.URL + "/sink",
    })
    moduleInstance.registerRatesHttpClientService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerReportExportHttpClientService(containerRegistrar{Container: containerInstance})

    client, resolveErr := melodycontainer.FromResolver[*httpclient.HttpClient](containerInstance, service.ServiceRatesHttpClient)
    if nil != resolveErr {
        t.Fatalf("resolve the rates client: %v", resolveErr)
    }

    response, requestErr := client.Get("latest")
    if nil != requestErr || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the provider reached, got %v, %v", response, requestErr)
    }

    if "/v1/latest" != requestedPath || "application/json" != acceptHeader {
        t.Fatalf("expected /v1/latest asked for json, got %q with accept %q", requestedPath, acceptHeader)
    }

    if _, byTypeErr := melodycontainer.FromResolverByType[*httpclient.HttpClient](containerInstance); nil == byTypeErr {
        t.Fatal("expected neither outbound client on the by-type index")
    }

    unwired := melodycontainer.NewContainer()
    moduleWithEnvironment(t, map[string]string{}).registerRatesHttpClientService(containerRegistrar{Container: unwired})
    if true == unwired.Has(service.ServiceRatesHttpClient) {
        t.Fatal("expected no rates client without a provider")
    }
}
