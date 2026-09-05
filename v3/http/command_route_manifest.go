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

/* the standard set joins the command's own so the machine-readable output has the quiet contract: without it the flag did not exist here, quiet could not suppress the run banner, and the manifest printed to stdout arrived framed inside it — unparseable from the first byte for the pipeline reading it */
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

/* Run emits the manifest. It mirrors the openapi generate command in the four places that decide whether the artifact is trustworthy, none of which it used to: a relative --out is anchored at the project directory rather than at whatever directory the process happened to start in, a target that is not a JSON document is refused rather than destroyed, the write lands through a temp file and a rename so an interrupted run leaves the previous manifest intact, and the output travels through the command writer rather than process stdout — the cli layer redirects that writer, and in json mode a raw print splices the document into the machine-readable stream. */
func (instance *RouteManifestCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    zone := strings.TrimSpace(commandContext.String("zone"))
    if "" != zone && false == IsRouteZone(zone) {
        /* an unrecognised zone matched no entry, so the command wrote an empty manifest over the good one and reported success; the frontend then failed to resolve every route it asked for, at runtime, with the build green */
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
