package cli

import (
    "fmt"

    "github.com/precision-soft/melody/v4/.example/reporting"
    melodyapplication "github.com/precision-soft/melody/v4/application"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
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

func (instance *ApplicationInformationCommand) Run(runtimeInstance melodycontainercontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    writer := commandContext.Writer

    configuration := melodyconfig.ConfigMustFromRuntime(runtimeInstance)
    fmt.Fprintln(writer, "env:", configuration.Kernel().Environment())

    processRole := melodyapplication.ProcessRoleMustFromRuntime(runtimeInstance)
    fmt.Fprintln(writer, "process_role:", processRole)
    fmt.Fprintln(writer, "background_work:", melodyconfig.RoleAllowsBackgroundWork(processRole))
    fmt.Fprintln(writer, "http_address:", configuration.Http().Address())
    fmt.Fprintln(writer, "public_directory:", configuration.Http().PublicDirectory())
    fmt.Fprintln(writer, "static_index_file:", configuration.Http().StaticIndexFile())

    router := melodyhttp.RouterMustFromRuntime(runtimeInstance)
    fmt.Fprintln(writer, "routes:", len(router.RouteDefinitions()))


    /* resolving the catalog report proves the generated wiring end to end: the service is registered only by melody:wiring:generate, and its provider pulls a collaborator from the container by type while reading three scalars from the parameters they are bound to. */
    catalogReport := melodycontainer.MustFromResolverByType[*reporting.CatalogReportService](runtimeInstance.Context(), runtimeInstance.Scope())
    fmt.Fprintln(writer, "catalog_report:", catalogReport.Title())
    fmt.Fprintln(writer, "catalog_report_page_size:", catalogReport.PageSize())
    fmt.Fprintln(writer, "catalog_report_refresh_interval:", catalogReport.RefreshInterval())

    return nil
}

var _ melodyclicontract.Command = (*ApplicationInformationCommand)(nil)
