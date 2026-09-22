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

/* Flags declares the quiet flag beside the command's own, defaulting to true as StandardFlags does: the document is the command's essential output and the run banner is decoration, and a command that declares no quiet flag keeps the banner it always had — around a document printed to stdout, that banner made the documented redirection write a file no parser read. */
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

        /* the auto-registration gate reads the registry service alone, so a container carrying it without the info service reaches this command and the tolerant resolver answers an empty Info — a document whose required title and version are empty strings. The document is still written, since the info is declared optional metadata, but the run says what the success would otherwise conceal — on the writer when the document goes to a file, and in the journal when the writer IS the document: with --out empty the command has one writer and prints the document on it, so a line of warning ahead of the json made the documented stdout mode (melody:openapi:generate > openapi.json) write a file no parser reads. */
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

    fmt.Fprintln(commandContext.Writer(), "wrote openapi document to", out)

    return nil
}

/* journal answers the application's logger, resolved through the runtime so the scope's logger wins over the root's, and the emergency logger when the runtime carries none — a process that generates the document without wiring a logger still has a journal of last resort. It resolves for itself rather than through the framework's LoggerFromRuntime, which files an emergency record of its own and answers nil where this door wants a fallback. The application's journal is also the wrong channel when it IS stdout: an empty kernel.log_path makes the container log to stdout, the writer the document goes to, so a record there lands ahead of the json exactly as the warning line did; that configuration is read here, and the emergency journal — stderr — carries the warning for it. */
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

/* journalSharesStdout reads the fact the container reads when it builds the logger: an empty log path means the journal writes to stdout. A runtime without the configuration, or one whose configuration does not resolve, answers false, so the application's logger is asked as before — this door must not be the one that fails a command written never to fail on its journal. What it cannot read is a logger the application substituted for the container's: the contract exposes no writer, so a substituted logger that writes to stdout under a non-empty log path still receives the warning, ahead of the document; keeping such a logger off stdout, or passing --out, is the application's side of that bargain, and OPENAPI.md says so. */
func journalSharesStdout(runtimeInstance runtimecontract.Runtime) bool {
    configuration, resolveErr := container.FromResolver[configcontract.Configuration](runtimeInstance.Container(), config.ServiceConfig)
    if nil != resolveErr || nil == configuration {
        return false
    }

    /* the kernel section is read through the same tolerance: a substitute configuration answering no kernel section is a door this command does not fail on either */
    kernelConfiguration := configuration.Kernel()
    if true == internal.IsNilInterface(kernelConfiguration) {
        return false
    }

    return "" == kernelConfiguration.LogPath()
}

var _ clicontract.Command = (*GenerateCommand)(nil)
