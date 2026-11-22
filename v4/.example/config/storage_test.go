package config

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyawss3 "github.com/precision-soft/melody/integrations/aws/s3/v4"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodystorage "github.com/precision-soft/melody/v4/storage"
)

/* with S3 configured the aws.s3 module builds the storage at boot and checks the bucket under the runtime that resolves it, warning rather than failing the boot, so the first upload after an outage checks it again */
func TestStorageModuleConfig_ChecksTheBucketUnderTheResolvingRuntimeAndWarns(t *testing.T) {
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

    moduleConfig := moduleInstance.storageModuleConfig()
    if false == moduleConfig.BucketCheck.Enabled || "eu-central-1" != moduleConfig.BucketCheck.Region || melodycontainer.BootFailureWarns != moduleConfig.BucketCheck.BootFailure {
        t.Fatalf("expected the bucket check enabled in the region, warning, got %+v", moduleConfig.BucketCheck)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodyawss3.NewModule(moduleConfig).RegisterServices(containerRegistrar{Container: containerInstance})

    names, modes := builtAtBootNames(containerInstance)
    if 1 != len(names) || melodystorage.ServiceStorage != names[0] || melodycontainer.BootFailureWarns != modes[names[0]] {
        t.Fatalf("expected the storage built at boot with the warning mode, got %v %v", names, modes)
    }

    bootContext, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    bootRuntime, finish := melodyruntime.NewBoot(bootContext, containerInstance)
    defer func() {
        _ = finish()
    }()

    refusalChannel := make(chan error, 1)
    go func() {
        _, checkErr := bootRuntime.Scope().Get(bootRuntime.Context(), melodystorage.ServiceStorage)
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

/* the example registers its local-disk storage only without S3, and builds nothing at boot for it */
func TestRegisterStorageServices_RegistersTheLocalStorageOnlyWithoutS3(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        values   map[string]string
        expected bool
    }{
        {"with s3", map[string]string{environmentKeyS3Endpoint: "127.0.0.1:1", environmentKeyS3AccessKey: "test", environmentKeyS3SecretKey: "test", environmentKeyS3Insecure: "true", environmentKeyS3Bucket: "melody-example"}, false},
        {"without s3", map[string]string{}, true},
    } {
        moduleInstance := moduleWithEnvironment(t, testCase.values)
        moduleInstance.buildStorage()

        containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
        moduleInstance.registerStorageServices(containerRegistrar{Container: containerInstance})

        if testCase.expected != containerInstance.Has(melodystorage.ServiceStorage) {
            t.Fatalf("%s: expected the local storage registered %v", testCase.name, testCase.expected)
        }

        if names, _ := builtAtBootNames(containerInstance); 0 != len(names) {
            t.Fatalf("%s: expected nothing built at boot, got %v", testCase.name, names)
        }
    }
}
