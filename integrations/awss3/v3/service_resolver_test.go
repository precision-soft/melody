package awss3

import (
    "strings"
    "testing"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodystorage "github.com/precision-soft/melody/v3/storage"
)

type recordingRegistrar struct {
    names []string
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
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

    RegisterStorageService(registrar, newOfflineTestClient(t), "example-bucket")

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

    RegisterStorageService(&recordingRegistrar{}, nil, "example-bucket")
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

    RegisterStorageService(&recordingRegistrar{}, newOfflineTestClient(t), "")
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
