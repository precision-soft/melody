package reporting

import (
    "strings"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "sync/atomic"
    "testing"
)

func TestCatalogReportExporterExport_SendsTheReadingToTheConfiguredSink(t *testing.T) {
    sink := newRecordingSink(t, http.StatusOK)

    exported, err := NewCatalogReportExporter(sink.server.URL+"/v1/report-sink").
        Export(exportRuntime(t), exportReading())
    if nil != err {
        t.Fatalf("the export failed: %v", err)
    }

    if false == exported {
        t.Error("the export reported that it sent nothing")
    }

    if int64(1) != sink.requests.Load() {
        t.Fatalf("the sink was given %d requests, wanted one", sink.requests.Load())
    }

    var received exportPayload
    if unmarshalErr := json.Unmarshal([]byte(sink.body.Load().(string)), &received); nil != unmarshalErr {
        t.Fatalf("the sink was sent a body that is not the export payload: %v", unmarshalErr)
    }

    if "2026-09-07T10:47:37Z" != received.RecordedAt {
        t.Errorf("the sink was told the reading was taken at %q", received.RecordedAt)
    }

    if "Melody Example Catalog: 25 entries" != received.Headline {
        t.Errorf("the sink was sent headline %q", received.Headline)
    }

    if "products=5 journal=3" != received.Payload {
        t.Errorf("the sink was sent payload %q", received.Payload)
    }
}

func TestCatalogReportExporterExport_FailsWhenTheSinkRefuses(t *testing.T) {
    sink := newRecordingSink(t, http.StatusServiceUnavailable)

    exported, err := NewCatalogReportExporter(sink.server.URL+"/v1/report-sink").
        Export(exportRuntime(t), exportReading())
    if nil == err {
        t.Fatal("a sink that refused the export was reported as having accepted it")
    }

    if true == exported {
        t.Error("a refused export reported that it sent something")
    }

    if int64(1) != sink.requests.Load() {
        t.Errorf("the sink was given %d requests, wanted one — the export does not retry", sink.requests.Load())
    }
}

func TestCatalogReportExporterExport_SendsNothingWithNoEndpointConfigured(t *testing.T) {
    sink := newRecordingSink(t, http.StatusOK)

    exported, err := NewCatalogReportExporter("").Export(exportRuntime(t), exportReading())
    if nil != err {
        t.Fatalf("an unconfigured export failed: %v", err)
    }

    if true == exported {
        t.Error("an unconfigured export reported that it sent something")
    }

    if int64(0) != sink.requests.Load() {
        t.Errorf("the sink was given %d requests with no endpoint configured", sink.requests.Load())
    }
}

func TestCatalogReportExporterExport_RefusesASinkThatRedirectsInsteadOfReceiving(t *testing.T) {
    sinkPosts := atomic.Int64{}
    pageGets := atomic.Int64{}

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        if "/v1/report-sink" == request.URL.Path {
            sinkPosts.Add(1)
            http.Redirect(writer, request, "/login", http.StatusFound)

            return
        }

        pageGets.Add(1)
        writer.WriteHeader(http.StatusOK)
    }))
    t.Cleanup(server.Close)

    exported, err := NewCatalogReportExporter(server.URL+"/v1/report-sink").
        Export(exportRuntime(t), exportReading())
    if nil == err {
        t.Fatal("a sink that redirected the export was reported as having accepted it")
    }

    if false == strings.Contains(err.Error(), "redirected the export") {
        t.Fatalf("expected the refusal to name the redirect, got %q", err.Error())
    }

    if true == exported {
        t.Error("a redirected export reported that it sent something")
    }

    if int64(1) != sinkPosts.Load() || int64(0) != pageGets.Load() {
        t.Errorf("the sink was posted %d times and the page read %d times, wanted one post and no read", sinkPosts.Load(), pageGets.Load())
    }
}
