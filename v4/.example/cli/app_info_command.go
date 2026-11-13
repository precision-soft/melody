package cli

import (
    "fmt"

    "github.com/precision-soft/melody/v4/.example/reporting"
    melodyapplication "github.com/precision-soft/melody/v4/application"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type ApplicationInformationCommand struct{}

func NewApplicationInformationCommand() *ApplicationInformationCommand {
    return &ApplicationInformationCommand{}
}

func (instance *ApplicationInformationCommand) Name() string {
    return "app:info"
}

func (instance *ApplicationInformationCommand) Description() string {
    return "prints application information"
}

func (instance *ApplicationInformationCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{}
}

func (instance *ApplicationInformationCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    writer := commandContext.Writer

    configuration := melodyconfig.ConfigMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container())
    fmt.Fprintln(writer, "env:", configuration.Kernel().Environment())

    processRole := melodyapplication.ProcessRoleMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container())
    fmt.Fprintln(writer, "process_role:", processRole)
    fmt.Fprintln(writer, "background_work:", melodyconfig.RoleAllowsBackgroundWork(processRole))
    fmt.Fprintln(writer, "http_address:", configuration.Http().Address())
    fmt.Fprintln(writer, "public_directory:", configuration.Http().PublicDirectory())
    fmt.Fprintln(writer, "static_index_file:", configuration.Http().StaticIndexFile())

    router := melodyhttp.RouterMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container())
    fmt.Fprintln(writer, "routes:", len(router.RouteDefinitions()))

    container := runtimeInstance.Container()
    fmt.Fprintln(writer, "services:", len(container.Names()))

    /* resolving the catalog report proves the generated wiring end to end: the service is registered only by melody:wiring:generate, and its provider pulls a collaborator from the container by type while reading three scalars from the parameters they are bound to. */
    catalogReport := melodycontainer.MustFromResolverByType[*reporting.CatalogReportService](runtimeInstance.Context(), container)
    fmt.Fprintln(writer, "catalog_report:", catalogReport.Headline())
    fmt.Fprintln(writer, "catalog_report_refresh_interval:", catalogReport.RefreshInterval())

    return nil
}

var _ melodyclicontract.Command = (*ApplicationInformationCommand)(nil)
