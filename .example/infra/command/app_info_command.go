package command

import (
	"fmt"

	melodyclicontract "github.com/precision-soft/melody/cli/contract"
	melodyconfig "github.com/precision-soft/melody/config"
	melodyhttp "github.com/precision-soft/melody/http"
	melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type AppInfoCommand struct{}

func NewAppInfoCommand() *AppInfoCommand {
	return &AppInfoCommand{}
}

func (instance *AppInfoCommand) Name() string {
	return "app:info"
}

func (instance *AppInfoCommand) Description() string {
	return "prints application information"
}

func (instance *AppInfoCommand) Flags() []melodyclicontract.Flag {
	return []melodyclicontract.Flag{}
}

func (instance *AppInfoCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
	configuration := melodyconfig.ConfigMustFromContainer(runtimeInstance.Container())
	fmt.Println("env:", configuration.Kernel().Env())
	fmt.Println("http_address:", configuration.Http().Address())
	fmt.Println("public_dir:", configuration.Http().PublicDir())
	fmt.Println("static_index_file:", configuration.Http().StaticIndexFile())

	router := melodyhttp.RouterMustFromContainer(runtimeInstance.Container())
	fmt.Println("routes:", len(router.RouteDefinitions()))

	container := runtimeInstance.Container()
	fmt.Println("services:", len(container.Names()))

	return nil
}

var _ melodyclicontract.Command = (*AppInfoCommand)(nil)
