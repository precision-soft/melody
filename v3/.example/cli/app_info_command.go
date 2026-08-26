package cli

import (
    "fmt"

    "github.com/precision-soft/melody/v3/.example/reporting"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

func (instance *AppInfoCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    configuration := melodyconfig.ConfigMustFromContainer(runtimeInstance.Container())
    fmt.Println("env:", configuration.Kernel().Env())

    processRole := melodyapplication.ProcessRoleMustFromContainer(runtimeInstance.Container())
    fmt.Println("process_role:", processRole)
    fmt.Println("background_work:", melodyconfig.RoleAllowsBackgroundWork(processRole))
    fmt.Println("http_address:", configuration.Http().Address())
    fmt.Println("public_dir:", configuration.Http().PublicDir())
    fmt.Println("static_index_file:", configuration.Http().StaticIndexFile())

    router := melodyhttp.RouterMustFromContainer(runtimeInstance.Container())
    fmt.Println("routes:", len(router.RouteDefinitions()))

    container := runtimeInstance.Container()
    fmt.Println("services:", len(container.Names()))

    /* resolving the catalog report proves the generated wiring end to end: the service is registered only by melody:wiring:generate, and its provider pulls a collaborator from the container by type while reading three scalars from the parameters they are bound to. */
    catalogReport := melodycontainer.MustFromResolverByType[*reporting.CatalogReportService](container)
    fmt.Println("catalog_report:", catalogReport.Headline())
    fmt.Println("catalog_report_refresh_interval:", catalogReport.RefreshInterval())

    return nil
}

var _ melodyclicontract.Command = (*AppInfoCommand)(nil)
