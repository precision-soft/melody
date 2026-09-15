package config

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v3/.example/service"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/httpclient"
)

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

    moduleWithRegisteredParameters(t, map[string]string{environmentKeyReportExportEndpoint: server.URL + "/sink"}).
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

func TestOutboundClientsFollowRegisteredEnvironmentParameters(t *testing.T) {
    for _, testCase := range []struct {
        name string
        values map[string]string
        rates bool
        export bool
    }{
        {name: "neither", values: map[string]string{}},
        {name: "rates", values: map[string]string{environmentKeyRatesBaseUrl: "https://rates.example.test/v1/"}, rates: true},
        {name: "export", values: map[string]string{environmentKeyReportExportEndpoint: "https://sink.example.test/report"}, export: true},
        {name: "both", values: map[string]string{environmentKeyRatesBaseUrl: "https://rates.example.test/v1/", environmentKeyReportExportEndpoint: "https://sink.example.test/report"}, rates: true, export: true},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            moduleInstance := moduleWithRegisteredParameters(t, testCase.values)
            containerInstance := melodycontainer.NewContainer()
            defer containerInstance.Close()
            registrar := containerRegistrar{Container: containerInstance}
            moduleInstance.registerRatesHttpClientService(registrar)
            moduleInstance.registerReportExportHttpClientService(registrar)
            if testCase.rates != containerInstance.Has(service.ServiceRatesHttpClient) || testCase.export != containerInstance.Has(service.ServiceReportExportHttpClient) {
                t.Fatal("outbound client wiring differs from environment")
            }
            for _, entry := range []struct { name string; enabled bool }{{service.ServiceRatesHttpClient, testCase.rates}, {service.ServiceReportExportHttpClient, testCase.export}} {
                if true == entry.enabled {
                    if _, err := melodycontainer.FromResolver[*httpclient.HttpClient](containerInstance, entry.name); nil != err {
                        t.Fatalf("resolve %s: %v", entry.name, err)
                    }
                }
            }
        })
    }
}
