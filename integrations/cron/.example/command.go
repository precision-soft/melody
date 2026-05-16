package example

import (
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
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

func (instance *BillingCleanupCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    return nil
}

var _ melodyclicontract.Command = (*BillingCleanupCommand)(nil)
