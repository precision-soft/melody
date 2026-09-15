package http

import (
    "encoding/json"
    "fmt"
    "path/filepath"
    "strings"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewRouteManifestCommand() *RouteManifestCommand {
    return &RouteManifestCommand{}
}

/* RouteManifestCommand emits the frontend route manifest (the exposed routes) as JSON, to a file or stdout. It mirrors the OpenAPI generate command so applications wire it the same way. */
type RouteManifestCommand struct {
}

func (instance *RouteManifestCommand) Name() string {
    return "melody:routes:manifest"
}

func (instance *RouteManifestCommand) Description() string {
    return "export the exposed routes as a JSON manifest for frontend URL generation"
}

/* Flags includes standard output flags so quiet and JSON modes suppress non-document output. */
func (instance *RouteManifestCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(output.StandardFlags(), []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "out",
            Usage: "path to write the route manifest to; prints to stdout when empty",
        },
        &clicontract.StringFlag{
            Name:  "zone",
            Usage: "restrict the manifest to a single zone (public, internal, frontend, client); all zones when empty",
        },
    })
}

/* Run exports the route manifest. Relative output paths are anchored at the project directory. Non-JSON targets are refused; file output uses a temporary file and rename. Console output uses the command writer. */
func (instance *RouteManifestCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    zone := strings.TrimSpace(commandContext.String("zone"))
    if "" != zone && false == IsRouteZone(zone) {

        return exception.NewError(
            "route zone is not one of the declared zones",
            map[string]any{
                "zone":          zone,
                "declaredZones": RouteZones(),
            },
            nil,
        )
    }

    router := RouterMustFromContainer(runtimeInstance.Container())

    manifest := BuildRouteManifest(router.RouteDefinitions())

    if "" != zone {
        manifest = FilterRouteManifestByZone(manifest, zone)
    }

    payload, marshalErr := json.MarshalIndent(manifest, "", "  ")
    if nil != marshalErr {
        return exception.NewError(
            "could not marshal the route manifest",
            map[string]any{"zone": zone},
            marshalErr,
        )
    }

    out := commandContext.String("out")
    if "" == out {
        fmt.Fprintln(commandContext.Writer(), string(payload))

        return nil
    }

    if false == filepath.IsAbs(out) {
        applicationConfiguration := config.ConfigMustFromContainer(runtimeInstance.Container())
        out = filepath.Join(applicationConfiguration.MustGet(config.KernelProjectDir).MustString(), out)
    }

    if refusalErr := internal.RefuseNonJsonOutputTarget(out, "route manifest"); nil != refusalErr {
        return refusalErr
    }

    if writeErr := internal.WriteFileAtomically(out, payload, "route manifest"); nil != writeErr {
        return writeErr
    }

    fmt.Fprintln(commandContext.Writer(), "wrote route manifest to", out)

    return nil
}

var _ clicontract.Command = (*RouteManifestCommand)(nil)
