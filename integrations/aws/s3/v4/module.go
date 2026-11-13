package awss3

import (
    "reflect"

    "github.com/minio/minio-go/v7"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* ModuleConfig registers the storage service over Client and Bucket, built from Storage; an empty one registers nothing, and a Bucket or a Storage with no Client fails the boot. */
type ModuleConfig struct {
    Client  *minio.Client
    Bucket  string
    Storage StorageConfig
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "aws.s3"
}

func (instance *Module) Description() string {
    return "registers the object storage service backed by an s3-compatible client"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if nil == instance.config.Client {
        if "" != instance.config.Bucket {
            exception.Panic(exception.NewError(
                "the aws.s3 module is handed a bucket and has no client",
                map[string]any{"field": "ModuleConfig.Bucket", "requires": "ModuleConfig.Client"},
                nil,
            ))
        }

        if false == reflect.ValueOf(instance.config.Storage).IsZero() {
            exception.Panic(exception.NewError(
                "the aws.s3 module is handed a storage configuration and has no client",
                map[string]any{"field": "ModuleConfig.Storage", "requires": "ModuleConfig.Client"},
                nil,
            ))
        }

        return
    }

    RegisterStorageService(registrar, instance.config.Client, instance.config.Bucket, instance.config.Storage)
}

/* the code is declared whether or not a client is handed, so a command's MELODY_CLI_BOOT_WARNINGS_IGNORED may name it in every environment the module is registered in */
func (instance *Module) RegisterBootWarnings(registrar applicationcontract.BootWarningRegistrar) {
    registrar.DeclareBootWarning(PlaintextEndpointBootWarningName, true)

    if nil == instance.config.Client {
        return
    }

    warning := plaintextEndpointWarningOf(instance.config.Client)
    if nil == warning || false == markPlaintextEndpointJudged(warning.endpoint) {
        return
    }

    registrar.AddBootWarning(warning.bootWarning())
}

var (
    _ applicationcontract.Module            = (*Module)(nil)
    _ applicationcontract.ServiceModule     = (*Module)(nil)
    _ applicationcontract.BootWarningModule = (*Module)(nil)
)
