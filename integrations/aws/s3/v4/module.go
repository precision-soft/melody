package awss3

import (
    "github.com/minio/minio-go/v7"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
)

/* ModuleConfig registers the storage service over Client and Bucket, built from Storage; a nil Client registers nothing. */
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
    return "awss3"
}

func (instance *Module) Description() string {
    return "registers the object storage service backed by an s3-compatible client"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    if nil == instance.config.Client {
        return
    }

    RegisterStorageService(registrar, instance.config.Client, instance.config.Bucket, instance.config.Storage)
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
