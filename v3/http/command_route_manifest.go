package http

import (
    "encoding/json"
    "fmt"
    "path/filepath"

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

/* RouteManifestCommand writes the frontend route manifest, the exposed routes, as JSON to a file or stdout, wired like the openapi generate command. */
type RouteManifestCommand struct {
}

func (instance *RouteManifestCommand) Name() string {
    return "melody:routes:manifest"
}

func (instance *RouteManifestCommand) Description() string {
    return "export the exposed routes as a JSON manifest for frontend URL generation"
}

/* the standard set gives the machine-readable output the quiet flag, so no banner frames it */
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

/* Run writes the manifest: a relative --out is anchored at the project directory, a target that is not a JSON document is refused rather than overwritten, the write lands through a temp file and a rename, and output goes through the command writer. */
func (instance *RouteManifestCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    /* the flag is handed over as typed, so FilterRouteManifestByZone is the one reader of a zone; its refusal lands before anything is written */
    zone := commandContext.String("zone")

    router := RouterMustFromContainer(runtimeInstance.Container())

    manifest := BuildRouteManifest(router.RouteDefinitions())

    manifest, filterErr := FilterRouteManifestByZone(manifest, zone)
    if nil != filterErr {
        return filterErr
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
