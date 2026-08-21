package openapi

import (
    "encoding/json"
    "fmt"
    "path/filepath"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/http"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewGenerateCommand(info Info, registry *Registry) *GenerateCommand {
    return &GenerateCommand{
        info:     info,
        registry: registry,
    }
}

/* NewGenerateCommandFromContainer builds the command so it resolves its Info and Registry from the service container at run time, letting the framework auto-register it without the application wiring those dependencies by hand. */
func NewGenerateCommandFromContainer() *GenerateCommand {
    return &GenerateCommand{resolveFromContainer: true}
}

type GenerateCommand struct {
    info                 Info
    registry             *Registry
    resolveFromContainer bool
}

func (instance *GenerateCommand) Name() string {
    return "melody:openapi:generate"
}

func (instance *GenerateCommand) Description() string {
    return "generate an OpenAPI 3 document from the registered routes"
}

func (instance *GenerateCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "out",
            Usage: "path to write the OpenAPI document to; prints to stdout when empty",
        },
    }
}

func (instance *GenerateCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    router := http.RouterMustFromContainer(runtimeInstance.Container())

    info := instance.info
    registry := instance.registry
    if true == instance.resolveFromContainer {
        info = InfoFromResolver(runtimeInstance.Container())
        registry = RegistryMustFromResolver(runtimeInstance.Container())

        /* the auto-registration gate reads the registry service alone, so a container carrying it without the info service reaches this command and the tolerant resolver answers an empty Info — a document whose required title and version are empty strings. The document is still written, since the info is declared optional metadata, but the run says what the success would otherwise conceal. */
        if false == runtimeInstance.Container().Has(ServiceOpenApiInfo) {
            fmt.Fprint(commandContext.Writer, "no openapi info service is registered; the document's info block is empty\n")
        }
    }

    document := Generate(info, router.RouteDefinitions(), registry)

    payload, marshalErr := json.MarshalIndent(document, "", "  ")
    if nil != marshalErr {
        return exception.NewError(
            "could not marshal the openapi document",
            map[string]any{
                "out": commandContext.String("out"),
            },
            marshalErr,
        )
    }

    out := commandContext.String("out")
    if "" == out {
        fmt.Fprintln(commandContext.Writer, string(payload))
        return nil
    }

    /* a relative path is anchored at the project directory, exactly as the wiring command anchors its own --out: the documented invocation is relative, and anchoring it at whatever directory the process happened to start in writes the document into a different tree per launcher while reporting success */
    if false == filepath.IsAbs(out) {
        applicationConfiguration := config.ConfigMustFromContainer(runtimeInstance.Container())
        out = filepath.Join(applicationConfiguration.MustGet(config.KernelProjectDir).MustString(), out)
    }

    /* the write below replaces the file whole; an existing file that is not a JSON document is someone's source a mistyped --out points at, not a previous output of this command */
    if refusalErr := internal.RefuseNonJsonOutputTarget(out, "openapi document"); nil != refusalErr {
        return refusalErr
    }

    if writeErr := internal.WriteFileAtomically(out, payload, "openapi document"); nil != writeErr {
        return writeErr
    }

    fmt.Fprintln(commandContext.Writer, "wrote openapi document to", out)

    return nil
}

var _ clicontract.Command = (*GenerateCommand)(nil)
