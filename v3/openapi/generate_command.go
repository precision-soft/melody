package openapi

import (
    "encoding/json"
    "fmt"
    "path/filepath"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewGenerateCommand(info Info, registry *Registry) *GenerateCommand {
    return &GenerateCommand{
        info:     info,
        registry: registry,
    }
}

/* NewGenerateCommandFromContainer builds the command so it resolves its Info and Registry from the service container at run time, which lets the framework register it without the application wiring either. */
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

/* Flags declares the quiet flag beside the command's own, defaulting to true: the document is the command's output, so a document printed to stdout carries no run banner and its redirection writes a file a parser reads. */
func (instance *GenerateCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "out",
            Usage: "path to write the OpenAPI document to; prints to stdout when empty",
        },
        &clicontract.BoolFlag{
            Name:  output.FlagNameQuiet,
            Usage: "suppress the run banner around the document (--quiet=false brings it back)",
            Value: true,
        },
        &clicontract.BoolFlag{
            Name:  output.FlagNameNoColor,
            Usage: "disable ansi colors",
            Value: false,
        },
    }
}

func (instance *GenerateCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    router := http.RouterMustFromContainer(runtimeInstance.Container())

    out := commandContext.String("out")

    info := instance.info
    registry := instance.registry
    if true == instance.resolveFromContainer {
        info = InfoFromResolver(runtimeInstance.Container())
        registry = RegistryMustFromResolver(runtimeInstance.Container())

        /* a container with the registry and no info service yields an empty Info; the document is still written and the run warns, on the writer when --out names a file and in the journal when the writer is the document itself */
        if false == runtimeInstance.Container().Has(ServiceOpenApiInfo) {
            warning := "no openapi info service is registered; the document's info block is empty"

            if "" == out {
                instance.journal(runtimeInstance).Warning(warning, loggingcontract.Context{"command": instance.Name()})
            } else {
                fmt.Fprint(commandContext.Writer(), warning+"\n")
            }
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

    if "" == out {
        fmt.Fprintln(commandContext.Writer(), string(payload))
        return nil
    }

    /* a relative path is anchored at the project directory, as the wiring command anchors its --out, so the document lands in one tree whatever directory the process started in */
    if false == filepath.IsAbs(out) {
        applicationConfiguration := config.ConfigMustFromContainer(runtimeInstance.Container())
        out = filepath.Join(applicationConfiguration.MustGet(config.KernelProjectDir).MustString(), out)
    }

    if refusalErr := internal.RefuseNonJsonOutputTarget(out, "openapi document"); nil != refusalErr {
        return refusalErr
    }

    if writeErr := internal.WriteFileAtomically(out, payload, "openapi document"); nil != writeErr {
        return writeErr
    }

    fmt.Fprintln(commandContext.Writer(), "wrote openapi document to", out)

    return nil
}

/* journal answers the runtime's logger, the scope's winning over the root's, and the emergency logger when the runtime carries none or when the journal writes to stdout, the writer the document goes to. */
func (instance *GenerateCommand) journal(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    if true == journalSharesStdout(runtimeInstance) {
        return logging.EmergencyLogger()
    }

    logger, resolveErr := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, logging.ServiceLogger)
    if nil != resolveErr || nil == logger {
        return logging.EmergencyLogger()
    }

    return logger
}

/* journalSharesStdout reports whether an empty kernel log path makes the journal write to stdout. A configuration it cannot read answers false, so the command never fails on its journal; a substituted logger that writes to stdout is the application's to keep off it. */
func journalSharesStdout(runtimeInstance runtimecontract.Runtime) bool {
    configuration, resolveErr := container.FromResolver[configcontract.Configuration](runtimeInstance.Container(), config.ServiceConfig)
    if nil != resolveErr || nil == configuration {
        return false
    }

    kernelConfiguration := configuration.Kernel()
    if true == internal.IsNilInterface(kernelConfiguration) {
        return false
    }

    return "" == kernelConfiguration.LogPath()
}

var _ clicontract.Command = (*GenerateCommand)(nil)
