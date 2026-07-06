package http

import (
    "encoding/json"
    "fmt"
    "os"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
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

func (instance *RouteManifestCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "out",
            Usage: "path to write the route manifest to; prints to stdout when empty",
        },
        &clicontract.StringFlag{
            Name:  "zone",
            Usage: "restrict the manifest to a single zone (public, internal, frontend, client); all zones when empty",
        },
    }
}

func (instance *RouteManifestCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    router := RouterMustFromContainer(runtimeInstance.Container())

    manifest := BuildRouteManifest(router.RouteDefinitions())

    zone := commandContext.String("zone")
    if "" != zone {
        manifest = filterManifestByZone(manifest, zone)
    }

    payload, marshalErr := json.MarshalIndent(manifest, "", "  ")
    if nil != marshalErr {
        return marshalErr
    }

    out := commandContext.String("out")
    if "" == out {
        fmt.Println(string(payload))

        return nil
    }

    writeErr := os.WriteFile(out, payload, 0o644)
    if nil != writeErr {
        return exception.NewError(
            "could not write the route manifest",
            map[string]any{"out": out},
            writeErr,
        )
    }

    fmt.Println("wrote route manifest to", out)

    return nil
}

func filterManifestByZone(manifest RouteManifest, zone string) RouteManifest {
    filtered := make([]RouteManifestEntry, 0, len(manifest.Routes))
    for _, entry := range manifest.Routes {
        if zone == entry.Zone {
            filtered = append(filtered, entry)
        }
    }

    return RouteManifest{Routes: filtered}
}

var _ clicontract.Command = (*RouteManifestCommand)(nil)
