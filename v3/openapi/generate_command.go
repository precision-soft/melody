package openapi

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
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
    existingContent, readErr := os.ReadFile(out)
    if nil != readErr && false == os.IsNotExist(readErr) {
        return exception.NewError(
            "could not inspect the existing output file",
            map[string]any{"out": out},
            readErr,
        )
    }
    if nil == readErr {
        trimmedContent := strings.TrimSpace(string(existingContent))
        if "" != trimmedContent && false == strings.HasPrefix(trimmedContent, "{") {
            return exception.NewError(
                "the output file exists and is not a JSON document; remove it or choose another path",
                map[string]any{"out": out},
                nil,
            )
        }
    }

    writeErr := writeDocumentFileAtomically(out, payload)
    if nil != writeErr {
        return writeErr
    }

    fmt.Fprintln(commandContext.Writer, "wrote openapi document to", out)

    return nil
}

/* writeDocumentFileAtomically lands the document through a temp file and a rename, so a write that dies partway — a full disk, a killed process — leaves the previous document intact instead of a torn JSON file published as the API's description. */
func writeDocumentFileAtomically(outputPath string, payload []byte) error {
    directoryPath := filepath.Dir(outputPath)

    makeDirectoryErr := os.MkdirAll(directoryPath, 0o755)
    if nil != makeDirectoryErr {
        return exception.NewError(
            "could not create the output directory of the openapi document",
            map[string]any{"out": outputPath},
            makeDirectoryErr,
        )
    }

    tempFile, tempErr := os.CreateTemp(directoryPath, filepath.Base(outputPath)+".*.tmp")
    if nil != tempErr {
        return exception.NewError(
            "could not create the temp file of the openapi document",
            map[string]any{"out": outputPath},
            tempErr,
        )
    }

    tempPath := tempFile.Name()

    _, writeErr := tempFile.Write(payload)
    if nil != writeErr {
        _ = tempFile.Close()
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not write the openapi document",
            map[string]any{"out": outputPath},
            writeErr,
        )
    }

    closeErr := tempFile.Close()
    if nil != closeErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not close the temp file of the openapi document",
            map[string]any{"out": outputPath},
            closeErr,
        )
    }

    /* the temp file is born 0600; the artifact keeps the mode a direct write would have given it */
    chmodErr := os.Chmod(tempPath, 0o644)
    if nil != chmodErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not set the mode of the openapi document",
            map[string]any{"out": outputPath},
            chmodErr,
        )
    }

    renameErr := os.Rename(tempPath, outputPath)
    if nil != renameErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not replace the output file with the openapi document",
            map[string]any{"out": outputPath},
            renameErr,
        )
    }

    return nil
}

var _ clicontract.Command = (*GenerateCommand)(nil)
