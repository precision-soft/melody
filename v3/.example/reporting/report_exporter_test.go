package reporting

import (
    "strings"
    "context"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/httpclient"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* recordingSink stands in for whatever an operator points the export at. It keeps the body it was given,
   because the property under test is that the reading travels — a status alone would be satisfied by an
   empty request. */
type recordingSink struct {
    server   *httptest.Server
    requests atomic.Int64
    body     atomic.Value
}

func newRecordingSink(t *testing.T, status int) *recordingSink {
    t.Helper()

    sink := &recordingSink{}
    sink.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        sink.requests.Add(1)

        received, _ := io.ReadAll(request.Body)
        sink.body.Store(string(received))

        writer.WriteHeader(status)
    }))

    t.Cleanup(sink.server.Close)

    return sink
}

/* exportRuntime carries the one service the exporter resolves, registered under the name the composition
   root registers it under — by NAME and not by type, which is what the application does too, because two
   clients of one concrete type cannot both hold the type. */
func exportRuntime(t *testing.T) melodyruntimecontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        containerInstance,
        service.ServiceReportExportHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            /* built the way the composition root builds it: a client that hands a redirect back rather than following it, which is what lets the exporter see the 3xx at all */
            return httpclient.NewHttpClient(httpclient.NewHttpClientConfig("", 2*time.Second, nil).WithoutRedirects()), nil
        },
        melodycontainer.WithoutTypeRegistration(),
    )

    t.Cleanup(func() { _ = containerInstance.Close() })

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func exportReading() *CatalogReading {
    return &CatalogReading{
        RecordedAt: time.Date(2026, time.September, 7, 10, 47, 37, 0, time.UTC),
        Headline:   "Melody Example Catalog: 25 entries",
        Payload:    "products=5 journal=3",
    }
}

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

/* the sink refusing is an error and not a quiet false, because the whole point of an export is that someone
   downstream received it — the command's exit code is the only way that reaches an operator */
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

/* the pair is what makes the gate a measurement rather than a montage that could not have sent anything:
   the arm above proves this runtime and this sink DO carry an export, so the nothing here is the gate's. */
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

/* a sink that moved, or a proxy in front of it that sends the caller to a login page, answers the POST with a redirect. Followed, net/http re-sends the POST as a GET without its body, and the 200 of the page it lands on used to read as "the sink received the reading" — exported=true over a sink that stored nothing. The export refuses the redirect by name, and the page is never asked. */
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
