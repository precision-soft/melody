package config

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

func TestBuildStorage_EndsTheBucketCheckWhenTheProcessContextEnds(t *testing.T) {
    released := make(chan struct{})
    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        <-released
    }))
    t.Cleanup(server.Close)
    t.Cleanup(func() { close(released) })

    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyS3Endpoint:  strings.TrimPrefix(server.URL, "http://"),
        environmentKeyS3AccessKey: "test",
        environmentKeyS3SecretKey: "test",
        environmentKeyS3Insecure:  "true",
        environmentKeyS3Region:    "eu-central-1",
        environmentKeyS3Bucket:    "melody-example",
    })

    processContext, cancel := context.WithCancel(context.Background())
    cancel()
    moduleInstance.processContext = processContext

    refusalChannel := make(chan error, 1)
    go func() {
        refusalChannel <- recoveredRefusal(t, moduleInstance.buildStorage)
    }()

    select {
    case refusal := <-refusalChannel:
        if false == errors.Is(refusal, context.Canceled) {
            t.Fatalf("expected the boot refused with the ended process context, got %v", refusal)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the bucket check to end with the process context")
    }
}

func TestBuildStorage_RefusesAnUnreadableInsecureSwitchBeforeItDials(t *testing.T) {
    requested := false
    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        requested = true
    }))
    t.Cleanup(server.Close)

    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyS3Endpoint: strings.TrimPrefix(server.URL, "http://"),
        environmentKeyS3Insecure: "treu",
        environmentKeyS3Bucket:   "melody-example",
    })
    moduleInstance.processContext = context.Background()

    refusal := recoveredRefusal(t, moduleInstance.buildStorage)
    if nil == refusal || false == strings.Contains(refusal.Error(), "the switch is neither true nor false") {
        t.Fatalf("expected the switch refused by name, got %v", refusal)
    }

    if true == requested {
        t.Fatal("expected no request before the switch is read")
    }
}
