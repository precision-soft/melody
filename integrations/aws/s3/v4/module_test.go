package awss3

import (
    "testing"

    "github.com/minio/minio-go/v7"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodystorage "github.com/precision-soft/melody/v4/storage"
)

type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
}

func (instance *spyServiceRegistrar) Register(serviceName string, provider any, options containercontract.RegisterOptions) error {
    instance.RegisterService(serviceName, provider, options)

    return nil
}

func (instance *spyServiceRegistrar) MustRegister(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.RegisterService(serviceName, provider, options)
}

type spyBootWarningRegistrar struct {
    declared map[string]bool
    warnings []applicationcontract.BootWarning
}

func (instance *spyBootWarningRegistrar) DeclareBootWarning(name string, writtenInCli bool) {
    if nil == instance.declared {
        instance.declared = make(map[string]bool)
    }

    instance.declared[name] = writtenInCli
}

func (instance *spyBootWarningRegistrar) AddBootWarning(warning applicationcontract.BootWarning) {
    instance.warnings = append(instance.warnings, warning)
}

func TestModule_RegisterBootWarningsDeclaresTheCodeAndAddsTheWarningForAPlaintextEndpointOffThisMachine(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        client   *minio.Client
        expected int
    }{
        {"no client", nil, 0},
        {"plaintext off this machine", newClientFor(t, "s3.example.com", false), 1},
        {"plaintext on a loopback address", newClientFor(t, "127.0.0.1:9000", false), 0},
        {"https", newClientFor(t, "s3.example.com", true), 0},
    } {
        forgetJudgedPlaintextEndpoints(t)

        registrar := &spyBootWarningRegistrar{}
        NewModule(ModuleConfig{Client: testCase.client, Bucket: "bucket"}).RegisterBootWarnings(registrar)

        if writtenInCli, declared := registrar.declared[PlaintextEndpointBootWarningName]; false == declared || false == writtenInCli || 1 != len(registrar.declared) {
            t.Fatalf("%s: expected the code declared as written in cli, got %v", testCase.name, registrar.declared)
        }

        if testCase.expected != len(registrar.warnings) {
            t.Fatalf("%s: expected %d warnings, got %v", testCase.name, testCase.expected, registrar.warnings)
        }

        if 1 == testCase.expected {
            warning := registrar.warnings[0]
            if PlaintextEndpointBootWarningName != warning.Name || "s3.example.com" != warning.Context["endpoint"] || 1 != len(warning.Context) || plaintextEndpointWarningMessage != warning.Message {
                t.Fatalf("%s: expected the warning naming the endpoint alone, got %v", testCase.name, warning)
            }
        }
    }

    forgetJudgedPlaintextEndpoints(t)

    client := newClientFor(t, "s3.example.com", false)
    NewModule(ModuleConfig{Client: client, Bucket: "bucket"}).RegisterBootWarnings(&spyBootWarningRegistrar{})

    registrar := &spyBootWarningRegistrar{}
    NewModule(ModuleConfig{Client: client, Bucket: "other"}).RegisterBootWarnings(registrar)
    if 0 != len(registrar.warnings) {
        t.Fatalf("expected an endpoint already named to add nothing, got %v", registrar.warnings)
    }
}

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "awss3" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "awss3")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterServices(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service without a client, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{Client: &minio.Client{}, Bucket: "bucket"}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodystorage.ServiceStorage != registrar.names[0] {
        t.Fatalf("expected the storage service, got %v", registrar.names)
    }
}

func TestModule_TheStorageConfigReachesTheRegistration(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatal("expected the module's refused storage config to fail the registration")
        }
    }()

    NewModule(ModuleConfig{Client: &minio.Client{}, Bucket: "bucket", Storage: StorageConfig{PutSpoolLimit: bound.UnlimitedCount()}}).RegisterServices(&spyServiceRegistrar{})
}

func TestModule_ABucketOrAStorageWithNoClientIsRefusedNamingIt(t *testing.T) {
    for field, config := range map[string]ModuleConfig{
        "ModuleConfig.Bucket":  {Bucket: "documents"},
        "ModuleConfig.Storage": {Storage: StorageConfig{PutSpoolLimit: bound.Of(1024)}},
    } {
        func() {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError || field != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
                    t.Fatalf("expected a refusal naming %s, got %v", field, recoveredErr)
                }
            }()

            NewModule(config).RegisterServices(&spyServiceRegistrar{})
        }()
    }
}
