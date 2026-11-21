package example

import (
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
)

type BillingCleanupCommand struct{}

func NewBillingCleanupCommand() *BillingCleanupCommand {
    return &BillingCleanupCommand{}
}

func (instance *BillingCleanupCommand) Name() string {
    return "billing:cleanup"
}

func (instance *BillingCleanupCommand) Description() string {
    return "drops expired billing tokens"
}

func (instance *BillingCleanupCommand) Flags() []melodyclicontract.Flag {
    return nil
}

func (instance *BillingCleanupCommand) Run(runtimeInstance melodycontainercontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    return nil
}

var _ melodyclicontract.Command = (*BillingCleanupCommand)(nil)
