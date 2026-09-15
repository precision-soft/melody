package wiring

import (
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "unicode"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* NewGenerateCommand builds the command that renders the service registrations for a bind set. It runs inside the application, so it checks every bind against the parameters the running configuration actually declares instead of against a copy of them. The output package must not be one the scanned constructors take types from: the generated file would import its own package. */
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
        &clicontract.StringFlag{
            Name:  "scoped-function",
            Usage: "name of the generated scope-owned registration function; defaults to the registration function name with Scoped appended, and is only emitted when a constructor carries //melody:scoped",
        },
        &clicontract.BoolFlag{
            Name:  "strict",
            Usage: "fail when a declared bind or exclude matched no constructor, or a constructor was skipped",
        },
        &clicontract.BoolFlag{
            Name:  "report-vendor",
            Usage: "name the vendor directories the scan stepped over",
        },
        &clicontract.StringFlag{
            Name:  "tags",
            Usage: "comma-separated build tags the target binary carries, so a constructor gated on one of them is scanned; the generated file is then specific to those tags and must be built with them, since it names constructors the untagged build does not have",
        },
        &clicontract.BoolFlag{
            Name:  "report-excluded",
            Usage: "name the build-excluded files that hold a constructor candidate",
        },
    }
}

func (instance *GenerateCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
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

    buildTags, buildTagsErr := splitBuildTags(commandContext.String("tags"))
    if nil != buildTagsErr {
        return buildTagsErr
    }

    source, report, generateErr := Generate(&GenerateRequest{
        ProjectDirectory:   projectDirectory,
        PackageName:        commandContext.String("package"),
        FunctionName:       commandContext.String("function"),
        ScopedFunctionName: commandContext.String("scoped-function"),
        BindSet:            instance.bindSet,
        DeclaredParameters: declaredParameters,
        BuildTags:          buildTags,
    })
    if nil != generateErr {
        return generateErr
    }

    instance.writeReport(commandContext, report)

    if true == commandContext.Bool("strict") {
        strictContext := make(map[string]any)

        if 0 < len(report.UnusedBinds) {
            strictContext["binds"] = strings.Join(report.UnusedBinds, ", ")
        }

        if 0 < len(report.UnusedExcludes) {
            strictContext["excludes"] = strings.Join(report.UnusedExcludes, ", ")
        }

        if 0 < len(report.Skipped) {
            skippedNames := make([]string, 0, len(report.Skipped))
            for _, skipped := range report.Skipped {
                skippedNames = append(skippedNames, skipped.Name)
            }

            strictContext["constructors"] = strings.Join(skippedNames, ", ")
        }

        if 0 < len(strictContext) {
            return exception.NewError(
                "declared binds or excludes matched no constructor, or constructors were skipped",
                strictContext,
                nil,
            )
        }
    }

    outputPath := commandContext.String("out")
    if "" == outputPath {
        fmt.Fprint(commandContext.Writer(), source)

        return nil
    }

    if false == filepath.IsAbs(outputPath) {
        outputPath = filepath.Join(projectDirectory, outputPath)
    }

    for _, packageBinding := range instance.bindSet.Packages() {
        scannedDirectory := packageBinding.Directory()
        if false == filepath.IsAbs(scannedDirectory) {
            scannedDirectory = filepath.Join(projectDirectory, scannedDirectory)
        }

        relativePath, relativeErr := filepath.Rel(scannedDirectory, outputPath)
        if nil == relativeErr && false == strings.HasPrefix(relativePath, "..") {
            return exception.NewError(
                "the output path lies inside a scanned package directory",
                map[string]any{
                    "out":        outputPath,
                    "importPath": packageBinding.ImportPath(),
                    "directory":  scannedDirectory,
                },
                nil,
            )
        }
    }

    existingContent, readErr := os.ReadFile(outputPath)
    if nil != readErr && false == os.IsNotExist(readErr) {
        return exception.NewError(
            "could not inspect the existing output file",
            map[string]any{
                "out": outputPath,
            },
            readErr,
        )
    }
    if nil == readErr && 0 < len(existingContent) && false == strings.HasPrefix(string(existingContent), generatedFileNote) {
        return exception.NewError(
            "the output file exists and is not a generated wiring file; remove it or choose another path",
            map[string]any{
                "out": outputPath,
            },
            nil,
        )
    }

    writeErr := internal.WriteFileAtomically(outputPath, []byte(source), "generated wiring file")
    if nil != writeErr {
        return writeErr
    }

    fmt.Fprintf(commandContext.Writer(), "wiring written to %s\n", outputPath)

    return nil
}

func (instance *GenerateCommand) writeReport(
    commandContext clicontract.Context,
    report *GenerateReport,
) {
    fmt.Fprintf(commandContext.Writer(), "registered %d constructors\n", report.ConstructorCount)

    if 0 < report.ScopedConstructorCount {
        fmt.Fprintf(commandContext.Writer(), "registered %d scoped constructors\n", report.ScopedConstructorCount)
    }

    for _, skipped := range report.Skipped {
        fmt.Fprintf(
            commandContext.Writer(),
            "skipped %s (%s:%d): %s\n",
            skipped.Name,
            skipped.File,
            skipped.Line,
            skipped.Reason,
        )
    }

    if true == commandContext.Bool("report-vendor") {
        for _, vendorDirectory := range report.SkippedVendorDirectories {
            fmt.Fprintf(commandContext.Writer(), "skipped vendor directory: %s\n", vendorDirectory)
        }
    }

    if true == commandContext.Bool("report-excluded") {
        for _, excludedFile := range report.ExcludedFiles {
            fmt.Fprintf(commandContext.Writer(), "excluded by build constraints (holds a constructor candidate): %s\n", excludedFile)
        }
    }

    for _, unused := range report.UnusedBinds {
        fmt.Fprintf(commandContext.Writer(), "bind %s matched no constructor argument\n", unused)
    }

    for _, unused := range report.UnusedExcludes {
        fmt.Fprintf(commandContext.Writer(), "exclude %s matched no constructor\n", unused)
    }

    if true == report.BindTargetsUnchecked {
        fmt.Fprint(commandContext.Writer(), "bind targets were not checked: the application declares no parameters\n")
    }

    reachedNames := make([]string, 0, len(report.GlobalBindReach))
    for argumentName := range report.GlobalBindReach {
        reachedNames = append(reachedNames, argumentName)
    }

    sort.Strings(reachedNames)

    for _, argumentName := range reachedNames {
        constructors := report.GlobalBindReach[argumentName]

        fmt.Fprintf(
            commandContext.Writer(),
            "global bind %s reaches %d constructors: %s\n",
            argumentName,
            len(constructors),
            strings.Join(constructors, ", "),
        )
    }
}

func splitBuildTags(tags string) ([]string, error) {
    if "" == tags {
        return nil, nil
    }

    buildTags := make([]string, 0)
    for _, tag := range strings.Split(tags, ",") {
        trimmedTag := strings.TrimSpace(tag)
        if "" == trimmedTag {
            continue
        }

        if false == isBuildTagIdentifier(trimmedTag) {
            return nil, exception.NewError(
                "a build tag must be a plain tag identifier, not a constraint expression",
                map[string]any{
                    "tag":  trimmedTag,
                    "tags": tags,
                },
                nil,
            )
        }

        buildTags = append(buildTags, trimmedTag)
    }

    return buildTags, nil
}

func isBuildTagIdentifier(tag string) bool {
    for _, character := range tag {
        if true == unicode.IsLetter(character) || true == unicode.IsDigit(character) {
            continue
        }

        if '_' == character || '.' == character {
            continue
        }

        return false
    }

    return true
}

var _ clicontract.Command = (*GenerateCommand)(nil)
