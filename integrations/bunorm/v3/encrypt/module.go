package encrypt

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/uptrace/bun"
)

type ModuleConfig struct {
    Database *bun.DB
    Cipher   Cipher
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "bunorm.encrypt"
}

func (instance *Module) Description() string {
    return "registers the melody:encrypt:database command for bulk encrypt, re-encrypt and decrypt"
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    if nil == instance.config.Database || nil == instance.config.Cipher {
        return nil
    }

    return Commands(instance.config.Database, instance.config.Cipher)
}

var (
    _ applicationcontract.Module    = (*Module)(nil)
    _ applicationcontract.CliModule = (*Module)(nil)
)
