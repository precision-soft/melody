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

    /* every strict violation is carried in one refusal: the run is inspected through its exit and its error record, and an error naming only the first violation found would attribute the failure to a bind typo while the lost constructor coverage beside it never crosses the process boundary */
    if true == commandContext.Bool("strict") {
        strictContext := make(map[string]any)

        if 0 < len(report.UnusedBinds) {
            strictContext["binds"] = strings.Join(report.UnusedBinds, ", ")
        }

        if 0 < len(report.UnusedExcludes) {
            strictContext["excludes"] = strings.Join(report.UnusedExcludes, ", ")
        }

        /* a skipped constructor is coverage the wiring silently lost; strict exists so a loss has to be acknowledged, which is what //melody:ignore is for */
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

    /* a generated file inside a scanned directory is read back by the next scan — with a package clause the surrounding sources do not carry, so the package stops compiling and the tool can no longer regenerate its way out; the constructor's own contract says the output package must not be a scanned one, and this is where it is enforceable */
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

    /* an existing file that does not open with the generated marker is someone's source, and the write below truncates before it writes; a mistyped --out must not be how a hand-written file dies */
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

    writeErr := writeGeneratedFileAtomically(outputPath, source)
    if nil != writeErr {
        return writeErr
    }

    fmt.Fprintf(commandContext.Writer(), "wiring written to %s\n", outputPath)

    return nil
}

/* writeGeneratedFileAtomically lands the generated source through a temp file and a rename, so a write that dies partway — a full disk, a killed process — leaves the previous file intact instead of a truncated Go source the compiler and the committed-file diff then read as the generator's output. */
func writeGeneratedFileAtomically(outputPath string, content string) error {
    directoryPath := filepath.Dir(outputPath)

    makeDirectoryErr := os.MkdirAll(directoryPath, 0o755)
    if nil != makeDirectoryErr {
        return exception.NewError(
            "could not create the output directory of the generated file",
            map[string]any{
                "out": outputPath,
            },
            makeDirectoryErr,
        )
    }

    tempFile, tempErr := os.CreateTemp(directoryPath, filepath.Base(outputPath)+".*.tmp")
    if nil != tempErr {
        return exception.NewError(
            "could not create the temp file of the generated file",
            map[string]any{
                "out": outputPath,
            },
            tempErr,
        )
    }

    tempPath := tempFile.Name()

    _, writeErr := tempFile.WriteString(content)
    if nil != writeErr {
        _ = tempFile.Close()
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not write the generated file",
            map[string]any{
                "out": outputPath,
            },
            writeErr,
        )
    }

    closeErr := tempFile.Close()
    if nil != closeErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not close the temp file of the generated file",
            map[string]any{
                "out": outputPath,
            },
            closeErr,
        )
    }

    /* the temp file is born 0600; the artifact keeps the mode a direct write would have given it */
    chmodErr := os.Chmod(tempPath, 0o644)
    if nil != chmodErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not set the mode of the generated file",
            map[string]any{
                "out": outputPath,
            },
            chmodErr,
        )
    }

    renameErr := os.Rename(tempPath, outputPath)
    if nil != renameErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not replace the output file with the generated file",
            map[string]any{
                "out": outputPath,
            },
            renameErr,
        )
    }

    return nil
}

/* writeReport prints what the generation covered and, more importantly, what it did not: a skipped constructor and an unmatched bind are both silent losses of coverage unless they are named. */
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

    /* vendor trees cannot contribute services, so naming them is opt-in: on a large project the list is noise, but a user wondering where a constructor went can ask for it */
    if true == commandContext.Bool("report-vendor") {
        for _, vendorDirectory := range report.SkippedVendorDirectories {
            fmt.Fprintf(commandContext.Writer(), "skipped vendor directory: %s\n", vendorDirectory)
        }
    }

    /* a build-excluded file holding a candidate is opt-in for the same reason: a foreign-GOOS variant is legitimate noise, but a user missing a service built under a tag can ask which files the scan left out and pass the tag through --tags */
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

    /* the generator's contract is to say when it could not check the bind targets; the command always hands over the running configuration, so this line names the degenerate case where that configuration declares nothing */
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

/* splitBuildTags parses the comma-separated tag list. A build context carries plain tag identifiers, not constraint expressions: a negation or a space-separated pair reaches it as a tag no file can ever declare, so the scan would silently behave as if nothing had been passed — the tagged files stay excluded, their services stay missing from the generated wiring, and strict still reports success. Reject the malformed entry here instead, where the mistake is still traceable to what was typed. */
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

/* isBuildTagIdentifier mirrors go/build's own isValidTag so a tag the go tool would accept is never rejected here: any unicode letter or digit, plus underscore and dot. */
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
