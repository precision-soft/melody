package config

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

func TestStorageBucket_IsCheckedAtBootUnderTheBootRuntimeAndEndsWithItsContext(t *testing.T) {
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
    moduleInstance.buildStorage()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    moduleInstance.registerStorageBucketService(containerRegistrar{Container: containerInstance})

    names, _ := builtAtBootNames(containerInstance)
    if 1 != len(names) || ServiceStorageBucket != names[0] {
        t.Fatalf("expected the bucket check registered to be built at boot, got %v", names)
    }

    bootContext, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    bootRuntime, finish := melodyruntime.NewBoot(bootContext, containerInstance)
    defer func() {
        _ = finish()
    }()

    refusalChannel := make(chan error, 1)
    go func() {
        _, checkErr := bootRuntime.Scope().Get(bootRuntime.Context(), ServiceStorageBucket)
        refusalChannel <- checkErr
    }()

    select {
    case refusal := <-refusalChannel:
        if false == errors.Is(refusal, context.DeadlineExceeded) {
            t.Fatalf("expected the bucket check ended with the boot runtime's context, got %v", refusal)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the bucket check to end with the boot runtime's context")
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

    refusal := recoveredRefusal(t, moduleInstance.buildStorage)
    if nil == refusal || false == strings.Contains(refusal.Error(), "the switch is neither true nor false") {
        t.Fatalf("expected the switch refused by name, got %v", refusal)
    }

    if true == requested {
        t.Fatal("expected no request before the switch is read")
    }
}

func TestRegisterStorageServices_RegistersTheBucketCheckBuiltAtBootOnlyWithS3Configured(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        values   map[string]string
        expected bool
    }{
        {"with s3", map[string]string{environmentKeyS3Endpoint: "127.0.0.1:1", environmentKeyS3AccessKey: "test", environmentKeyS3SecretKey: "test", environmentKeyS3Insecure: "true", environmentKeyS3Bucket: "melody-example"}, true},
        {"without s3", map[string]string{}, false},
    } {
        moduleInstance := moduleWithEnvironment(t, testCase.values)
        moduleInstance.buildStorage()

        containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
        moduleInstance.registerStorageServices(containerRegistrar{Container: containerInstance})

        names, _ := builtAtBootNames(containerInstance)
        found := false
        for _, name := range names {
            found = found || ServiceStorageBucket == name
        }

        if testCase.expected != found {
            t.Fatalf("%s: expected the bucket check built at boot %v, got names %v", testCase.name, testCase.expected, names)
        }
    }
}
