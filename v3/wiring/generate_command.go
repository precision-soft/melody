package wiring

import (
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* NewGenerateCommand builds the command that renders the service registrations for a bind set. It runs inside the application, so it checks every bind against the parameters the running configuration actually declares instead of against a copy of them. */
func NewGenerateCommand(bindSet *BindSet) *GenerateCommand {
    return &GenerateCommand{
        bindSet: bindSet,
    }
}

type GenerateCommand struct {
    bindSet *BindSet
}

func (instance *GenerateCommand) Name() string {
    return "melody:wiring:generate"
}

func (instance *GenerateCommand) Description() string {
    return "generate the container registrations for the scanned packages"
}

func (instance *GenerateCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "out",
            Usage: "path to write the generated file to; prints to stdout when empty",
        },
        &clicontract.StringFlag{
            Name:  "package",
            Usage: "package name of the generated file",
            Value: "config",
        },
        &clicontract.StringFlag{
            Name:  "function",
            Usage: "name of the generated registration function",
            Value: "RegisterGeneratedServices",
        },
        &clicontract.BoolFlag{
            Name:  "strict",
            Usage: "fail when a declared bind matched no constructor argument",
        },
    }
}

func (instance *GenerateCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    if nil == instance.bindSet {
        return exception.NewError("the wiring generate command requires a bind set", nil, nil)
    }

    applicationConfiguration := config.ConfigMustFromContainer(runtimeInstance.Container())

    projectDirectory := applicationConfiguration.MustGet(config.KernelProjectDir).MustString()

    declaredParameters := make(map[string]bool)
    for _, name := range applicationConfiguration.Names() {
        declaredParameters[name] = true
    }

    source, report, generateErr := Generate(&GenerateRequest{
        ProjectDirectory:   projectDirectory,
        PackageName:        commandContext.String("package"),
        FunctionName:       commandContext.String("function"),
        BindSet:            instance.bindSet,
        DeclaredParameters: declaredParameters,
    })
    if nil != generateErr {
        return generateErr
    }

    instance.writeReport(commandContext, report)

    if true == commandContext.Bool("strict") && 0 < len(report.UnusedBinds) {
        return exception.NewError(
            "declared binds matched no constructor argument",
            map[string]any{
                "binds": strings.Join(report.UnusedBinds, ", "),
            },
            nil,
        )
    }

    out := commandContext.String("out")
    if "" == out {
        fmt.Fprint(commandContext.Writer, source)

        return nil
    }

    outputPath := out
    if false == filepath.IsAbs(outputPath) {
        outputPath = filepath.Join(projectDirectory, outputPath)
    }

    writeErr := os.WriteFile(outputPath, []byte(source), 0o644)
    if nil != writeErr {
        return exception.NewError(
            "could not write the generated wiring",
            map[string]any{
                "out": outputPath,
            },
            writeErr,
        )
    }

    fmt.Fprintf(commandContext.Writer, "wiring written to %s\n", outputPath)

    return nil
}

/* writeReport prints what the generation covered and, more importantly, what it did not: a skipped constructor and an unmatched bind are both silent losses of coverage unless they are named. */
func (instance *GenerateCommand) writeReport(
    commandContext *clicontract.CommandContext,
    report *GenerateReport,
) {
    fmt.Fprintf(commandContext.Writer, "registered %d constructors\n", report.ConstructorCount)

    for _, skipped := range report.Skipped {
        fmt.Fprintf(
            commandContext.Writer,
            "skipped %s (%s:%d): %s\n",
            skipped.Name,
            skipped.File,
            skipped.Line,
            skipped.Reason,
        )
    }

    for _, unused := range report.UnusedBinds {
        fmt.Fprintf(commandContext.Writer, "bind %s matched no constructor argument\n", unused)
    }

    reachedNames := make([]string, 0, len(report.GlobalBindReach))
    for argumentName := range report.GlobalBindReach {
        reachedNames = append(reachedNames, argumentName)
    }

    sort.Strings(reachedNames)

    for _, argumentName := range reachedNames {
        constructors := report.GlobalBindReach[argumentName]

        fmt.Fprintf(
            commandContext.Writer,
            "global bind %s reaches %d constructors: %s\n",
            argumentName,
            len(constructors),
            strings.Join(constructors, ", "),
        )
    }
}

var _ clicontract.Command = (*GenerateCommand)(nil)
