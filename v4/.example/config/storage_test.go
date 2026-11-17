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

func TestObjectStorageIsInsecure_DialsPlaintextOnlyWhenTheInsecureSwitchIsExactlyTrue(t *testing.T) {
    for insecureValue, insecure := range map[string]bool{
        "":      false,
        "false": false,
        "TRUE":  false,
        "1":     false,
        "true":  true,
    } {
        if insecure != objectStorageIsInsecure(insecureValue) {
            t.Fatalf("expected S3_INSECURE=%q to dial insecure=%v", insecureValue, insecure)
        }
    }
}

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
