package config

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

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
