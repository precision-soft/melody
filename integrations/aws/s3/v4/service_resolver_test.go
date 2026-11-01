package awss3

import (
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

type recordingRegistrar struct {
    names     []string
    providers []any
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
    instance.providers = append(instance.providers, provider)
}

/* newOfflineTestClient builds a client without dialing anything: minio.New only parses the endpoint. */
func newOfflineTestClient(t *testing.T) *minio.Client {
    t.Helper()

    client, clientErr := minio.New("localhost:9000", &minio.Options{
        Creds: credentials.NewStaticV4("test-access", "test-secret", ""),
    })
    if nil != clientErr {
        t.Fatalf("could not build the offline test client: %v", clientErr)
    }

    return client
}

func TestRegisterStorageServiceUsesCoreStorageName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterStorageService(registrar, newOfflineTestClient(t), "example-bucket", StorageConfig{})

    if 0 == len(registrar.names) || melodystorage.ServiceStorage != registrar.names[0] {
        t.Fatalf("expected %q to be registered, got %v", melodystorage.ServiceStorage, registrar.names)
    }
}

func TestRegisterStorageServiceRefusesANilClientAtRegistration(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected the nil client to be refused at registration, before any provider closure runs")
        }

        if false == strings.Contains(recoveredText(recovered), "client is nil") {
            t.Fatalf("expected the refusal to name the nil client, got %v", recovered)
        }
    }()

    RegisterStorageService(&recordingRegistrar{}, nil, "example-bucket", StorageConfig{})
}

func TestRegisterStorageServiceRefusesAnEmptyBucketAtRegistration(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected the empty bucket to be refused at registration, before any provider closure runs")
        }

        if false == strings.Contains(recoveredText(recovered), "bucket is empty") {
            t.Fatalf("expected the refusal to name the empty bucket, got %v", recovered)
        }
    }()

    RegisterStorageService(&recordingRegistrar{}, newOfflineTestClient(t), "", StorageConfig{})
}

func recoveredText(recovered any) string {
    recoveredErr, isError := recovered.(error)
    if true == isError {
        return recoveredErr.Error()
    }

    text, isText := recovered.(string)
    if true == isText {
        return text
    }

    return ""
}

func TestRegisterStorageServiceRefusesALimitOutsideItsRuleAtRegistration(t *testing.T) {
    registrar := &recordingRegistrar{}

    func() {
        defer func() {
            recovered := recover()
            if false == strings.Contains(fmt.Sprintf("%v", exception.LogContext(recoveredError(recovered), exception.LogContextConfig{})), "StorageConfig.PutSpoolLimit") {
                t.Fatalf("expected a refusal naming StorageConfig.PutSpoolLimit, got %v", recovered)
            }
        }()

        RegisterStorageService(registrar, newOfflineTestClient(t), "example-bucket", StorageConfig{PutSpoolLimit: bound.Of(0)})
    }()

    if 0 != len(registrar.names) {
        t.Fatalf("expected nothing registered for a refused config, got %v", registrar.names)
    }
}

func TestRegisterStorageServiceHandsTheGivenConfigToTheStorage(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterStorageService(registrar, newOfflineTestClient(t), "example-bucket", StorageConfig{PutSpoolLimit: bound.Of(1024), MultipartAbortTimeout: bound.Of(time.Second)})

    factory, isFactory := registrar.providers[0].(func(resolver containercontract.Resolver) (storagecontract.Storage, error))
    if false == isFactory {
        t.Fatalf("expected the storage factory, got %T", registrar.providers[0])
    }

    built, _ := factory(nil)

    storage, isStorage := built.(*Storage)
    if false == isStorage || 1024 != storage.config.putSpoolLimit || time.Second != storage.config.multipartAbortTimeout {
        t.Fatalf("expected the given config on the storage, got %#v", built)
    }
}

func recoveredError(recovered any) error {
    recoveredErr, _ := recovered.(error)

    return recoveredErr
}
