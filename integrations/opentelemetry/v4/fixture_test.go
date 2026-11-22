package opentelemetry

import (
    "bufio"
    "context"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/metric/metricdata"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* conventionDurationBoundaries is the HTTP semantic conventions' advisory bucket list for http.server.request.duration, written out apart from the production default it pins */
var conventionDurationBoundaries = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

func testRequestAndRuntime() (httpcontract.Request, containercontract.Runtime) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/orders/42", nil)
    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{})

    return request, runtimeInstance
}

func okHandler() httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{"ok": true})
    }
}

/* streamingTestWriter is a writer that can take a write deadline and be hijacked, so a test can require the recording writer to leave both reachable to the handler behind it */
type streamingTestWriter struct {
    *httptest.ResponseRecorder
    deadlineSet bool
    hijacked    bool
}

func (instance *streamingTestWriter) SetWriteDeadline(deadline time.Time) error {
    instance.deadlineSet = true

    return nil
}

func (instance *streamingTestWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    instance.hijacked = true

    return nil, nil, nil
}

/* collectHistogram reads the named histogram off a manual reader, failing the test when it is absent */
func collectHistogram(t *testing.T, reader *sdkmetric.ManualReader, name string) (metricdata.Metrics, metricdata.HistogramDataPoint[float64]) {
    t.Helper()

    var collected metricdata.ResourceMetrics
    if collectErr := reader.Collect(context.Background(), &collected); nil != collectErr {
        t.Fatalf("collect: %v", collectErr)
    }

    for _, scope := range collected.ScopeMetrics {
        for _, instrument := range scope.Metrics {
            if name != instrument.Name {
                continue
            }

            histogram, isHistogram := instrument.Data.(metricdata.Histogram[float64])
            if false == isHistogram || 1 != len(histogram.DataPoints) {
                t.Fatalf("expected one histogram point under %s, got %#v", name, instrument.Data)
            }

            return instrument, histogram.DataPoints[0]
        }
    }

    t.Fatalf("expected an instrument named %s", name)

    return metricdata.Metrics{}, metricdata.HistogramDataPoint[float64]{}
}

func instrumentNames(t *testing.T, reader *sdkmetric.ManualReader) []string {
    t.Helper()

    var collected metricdata.ResourceMetrics
    if collectErr := reader.Collect(context.Background(), &collected); nil != collectErr {
        t.Fatalf("collect: %v", collectErr)
    }

    names := make([]string, 0)
    for _, scope := range collected.ScopeMetrics {
        for _, instrument := range scope.Metrics {
            names = append(names, instrument.Name)
        }
    }

    return names
}
