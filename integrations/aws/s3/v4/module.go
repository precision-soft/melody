package awss3

import (
    "reflect"

    "github.com/minio/minio-go/v7"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* ModuleConfig registers the storage service over Client and Bucket, built from Storage; an empty one registers nothing, and a Bucket, a Storage or a BucketCheck with no Client fails the boot. */
type ModuleConfig struct {
    Client      *minio.Client
    Bucket      string
    Storage     StorageConfig
    BucketCheck BucketCheckConfig
}

/* BucketCheckConfig has the storage's provider ensure the bucket, under the runtime of whoever resolves the storage, before it answers the storage; the storage is then built at boot by an http process, and a command checks the bucket at its first use of the storage unless the mode is BootFailureFailsBoot. BootFailure is the mode of that build: the zero value warns at boot and has the first use of the storage check the bucket again, BootFailureFailsBoot stops the boot on a bucket that cannot be ensured. Region is the region a missing bucket is created in. A Region or a BootFailure without Enabled is refused. */
type BucketCheckConfig struct {
    Enabled     bool
    Region      string
    BootFailure containercontract.BootFailureMode
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

        if false == reflect.ValueOf(instance.config.BucketCheck).IsZero() {
            exception.Panic(exception.NewError(
                "the aws.s3 module is handed a bucket check and has no client",
                map[string]any{"field": "ModuleConfig.BucketCheck", "requires": "ModuleConfig.Client"},
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

    bucketCheck := instance.config.BucketCheck
    if false == bucketCheck.Enabled && false == reflect.ValueOf(bucketCheck).IsZero() {
        exception.Panic(exception.NewError(
            "the aws.s3 module's bucket check is configured and not enabled",
            map[string]any{"field": "ModuleConfig.BucketCheck", "requires": "ModuleConfig.BucketCheck.Enabled"},
            nil,
        ))
    }

    registerStorageService(registrar, instance.config.Client, instance.config.Bucket, instance.config.Storage, bucketCheck)
}

/* the code is declared whether or not a client is handed, so a command's MELODY_CLI_BOOT_WARNINGS_IGNORED may name it in every environment the module is registered in */
func (instance *Module) RegisterBootWarnings(registrar applicationcontract.BootWarningRegistrar) {
    registrar.DeclareBootWarning(PlaintextEndpointBootWarningName, applicationcontract.BootWarningConfig{WrittenInCli: true})

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
