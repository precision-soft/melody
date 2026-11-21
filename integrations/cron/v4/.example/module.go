package example

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v4"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

/* NewCronModule answers the cron module the application registers beside NewModule: it schedules the billing commands and installs the ansible-cron dialect, selectable by melody:cron:generate --template ansible-cron, whose ownership line names the application the generator runs for. */
func NewCronModule() *melodycron.Module {
    return melodycron.NewModule(melodycron.ModuleConfig{
        Configuration: func(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
            return newCronConfiguration()
        },
        Templates: []melodycron.Template{
            &AnsibleCronTemplate{TaskNamePrefix: "billing cron: "},
        },
    })
}

type Module struct{}

func NewModule() *Module {
    return &Module{}
}

func (instance *Module) Name() string {
    return "billing"
}

func (instance *Module) Description() string {
    return "billing CLI commands with cron scheduling"
}

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        NewBillingCleanupCommand(),
    }
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
var _ melodyapplicationcontract.CliModule = (*Module)(nil)
