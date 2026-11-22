package awss3

import (
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

type containerServiceRegistrar struct {
    containercontract.Container
}

func (instance containerServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.Container.MustRegister(serviceName, provider, options)
}

func resolveStorageWithin(t *testing.T, serviceContainer containercontract.Container, budget time.Duration) error {
    t.Helper()

    runContext, cancel := context.WithTimeout(t.Context(), budget)
    defer cancel()

    _, resolveErr := runtime.FromRuntime[storagecontract.Storage](runtime.NewRun(runContext, serviceContainer.NewScope()), melodystorage.ServiceStorage)

    return resolveErr
}

func TestModule_ABucketCheckEnsuresTheBucketBeforeAnsweringTheStorageAndIsBuiltAtBootWithItsMode(t *testing.T) {
    for _, mode := range []containercontract.BootFailureMode{containercontract.BootFailureWarns, containercontract.BootFailureFailsBoot} {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        NewModule(ModuleConfig{
            Client:      newClientFor(t, "127.0.0.1:1", false),
            Bucket:      "bucket",
            BucketCheck: BucketCheckConfig{Enabled: true, Region: "us-east-1", BootFailure: mode},
        }).RegisterServices(containerServiceRegistrar{Container: serviceContainer})

        registrations := serviceContainer.(interface {
            BuiltAtBootRegistrations() []containercontract.BuiltAtBootRegistration
        }).BuiltAtBootRegistrations()
        if 1 != len(registrations) || melodystorage.ServiceStorage != registrations[0].ServiceName || mode != registrations[0].BootFailure {
            t.Fatalf("%s: expected the storage built at boot with the declared mode, got %v", mode, registrations)
        }

        resolveErr := resolveStorageWithin(t, serviceContainer, 2*time.Second)
        if nil == resolveErr || false == strings.Contains(strings.Join(exception.BuildCauseChain(resolveErr, 16), " | "), "object storage bucket check failed") {
            t.Fatalf("%s: expected the storage refused over the bucket check, got %v", mode, resolveErr)
        }
    }
}

func TestModule_WithoutABucketCheckTheStorageCallsNoS3AtBuild(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{Client: newClientFor(t, "127.0.0.1:1", false), Bucket: "bucket"}).RegisterServices(containerServiceRegistrar{Container: serviceContainer})

    if 0 != len(serviceContainer.(interface {
        BuiltAtBootRegistrations() []containercontract.BuiltAtBootRegistration
    }).BuiltAtBootRegistrations()) {
        t.Fatalf("expected the storage not built at boot")
    }

    if resolveErr := resolveStorageWithin(t, serviceContainer, 2*time.Second); nil != resolveErr {
        t.Fatalf("expected the storage built without reaching S3, got %v", resolveErr)
    }
}

func TestModule_ABucketCheckWithoutAClientOrNotEnabledIsRefused(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        config   ModuleConfig
        expected string
    }{
        {"no client", ModuleConfig{BucketCheck: BucketCheckConfig{Enabled: true}}, "the aws.s3 module is handed a bucket check and has no client"},
        {"a region not enabled", ModuleConfig{Client: newClientFor(t, "127.0.0.1:1", false), Bucket: "bucket", BucketCheck: BucketCheckConfig{Region: "us-east-1"}}, "the aws.s3 module's bucket check is configured and not enabled"},
        {"a mode not enabled", ModuleConfig{Client: newClientFor(t, "127.0.0.1:1", false), Bucket: "bucket", BucketCheck: BucketCheckConfig{BootFailure: containercontract.BootFailureFailsBoot}}, "the aws.s3 module's bucket check is configured and not enabled"},
    } {
        func() {
            defer func() {
                recovered := recover()
                refusal, isRefusal := recovered.(*exception.Error)
                if false == isRefusal || testCase.expected != refusal.Message() {
                    t.Fatalf("%s: expected %q, got %v", testCase.name, testCase.expected, recovered)
                }
            }()

            NewModule(testCase.config).RegisterServices(containerServiceRegistrar{Container: container.NewContainer(container.ContainerConfig{})})
        }()
    }
}

/* a missing bucket is created in the region the check declares */
func TestModule_ABucketCheckCreatesAMissingBucketInItsRegion(t *testing.T) {
    var createBody atomic.Value
    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        if nethttp.MethodPut == request.Method {
            body, _ := io.ReadAll(request.Body)
            createBody.Store(string(body))
            writer.WriteHeader(nethttp.StatusOK)

            return
        }

        writer.WriteHeader(nethttp.StatusNotFound)
    }))
    t.Cleanup(server.Close)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{
        Client:      newClientFor(t, strings.TrimPrefix(server.URL, "http://"), false),
        Bucket:      "bucket",
        BucketCheck: BucketCheckConfig{Enabled: true, Region: "eu-central-1"},
    }).RegisterServices(containerServiceRegistrar{Container: serviceContainer})

    if resolveErr := resolveStorageWithin(t, serviceContainer, 5*time.Second); nil != resolveErr {
        t.Fatalf("expected the storage answered after the bucket was created, got %v", resolveErr)
    }

    body, _ := createBody.Load().(string)
    if false == strings.Contains(body, "eu-central-1") {
        t.Fatalf("expected the bucket created in the declared region, got %q", body)
    }
}
