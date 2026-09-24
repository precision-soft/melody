package wiring

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "unicode"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
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

/* Flags declares the quiet flag beside the command's own, defaulting to true as StandardFlags does: the generated source is the command's essential output, and a banner around a source printed to stdout would be the first bytes of the file. */
func (instance *GenerateCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "out",
            Usage: "path to write the generated file to; prints to stdout when empty",
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

    /* the report goes on the writer when the source goes to a file, and into the journal when the writer is the source, so the stdout mode prints a file that compiles */
    reportWriter := commandContext.Writer()
    journalWriter := (*journalLineWriter)(nil)
    if "" == commandContext.String("out") {
        journalWriter = &journalLineWriter{logger: instance.journal(runtimeInstance), command: instance.Name()}
        reportWriter = journalWriter
    }

    instance.writeReport(reportWriter, commandContext, report)

    if nil != journalWriter {
        journalWriter.flush()
    }

    /* every strict violation is carried in one refusal, so the error record names all the lost coverage and not only the first violation found */
    if true == commandContext.Bool("strict") {
        strictContext := make(map[string]any)

        if 0 < len(report.UnusedBinds) {
            strictContext["binds"] = strings.Join(report.UnusedBinds, ", ")
        }

        if 0 < len(report.UnusedExcludes) {
            strictContext["excludes"] = strings.Join(report.UnusedExcludes, ", ")
        }

        /* a skipped constructor is coverage the wiring lost; strict makes it be acknowledged, which is what //melody:ignore is for */
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

    /* a generated file inside a scanned directory would be read back by the next scan with a package clause the sources there do not carry, and the package would stop compiling, so the output must not be inside a scanned directory. Containment is read on path components, as the static file server reads it, and a relative path the two cannot be related on is refused. */
    for _, packageBinding := range instance.bindSet.Packages() {
        scannedDirectory := packageBinding.Directory()
        if false == filepath.IsAbs(scannedDirectory) {
            scannedDirectory = filepath.Join(projectDirectory, scannedDirectory)
        }

        relativePath, relativeErr := filepath.Rel(scannedDirectory, outputPath)
        if nil != relativeErr {
            return exception.NewError(
                "the output path cannot be related to a scanned package directory",
                map[string]any{
                    "out":        outputPath,
                    "importPath": packageBinding.ImportPath(),
                    "directory":  scannedDirectory,
                },
                relativeErr,
            )
        }

        liesOutside := ".." == relativePath || true == strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
        if false == liesOutside {
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

    /* an existing file that does not open with the generated marker is someone's source, and the write below truncates it; a mistyped --out must not destroy it */
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

    /* written through the framework's atomic writer, so a write that dies partway leaves the previous file intact */
    writeErr := internal.WriteFileAtomically(outputPath, []byte(source), "generated wiring file")
    if nil != writeErr {
        return writeErr
    }

    fmt.Fprintf(commandContext.Writer(), "wiring written to %s\n", outputPath)

    return nil
}

/* journalLineWriter carries the report into the journal, one record per line, in stdout mode, where the stream is the generated source. The count of what was registered is information; every other line names coverage the wiring lost and is a warning, so a journal read at warning keeps it. */
type journalLineWriter struct {
    logger  loggingcontract.Logger
    command string
    pending string

    /* the reach lines are collected and journaled by flush as one record */
    globalBindReach []string
}

func (instance *journalLineWriter) Write(payload []byte) (int, error) {
    instance.pending = instance.pending + string(payload)

    for {
        lineEnd := strings.IndexByte(instance.pending, '\n')
        if 0 > lineEnd {
            break
        }

        line := instance.pending[:lineEnd]
        instance.pending = instance.pending[lineEnd+1:]
        instance.journalLine(line)
    }

    return len(payload), nil
}

/* flush journals a last line written without its line end, which the line loop keeps pending */
func (instance *journalLineWriter) flush() {
    line := instance.pending
    instance.pending = ""
    instance.journalLine(line)

    if 0 == len(instance.globalBindReach) {
        return
    }

    reaches := instance.globalBindReach
    instance.globalBindReach = nil

    instance.logger.Warning(
        fmt.Sprintf("%d global %s reach constructors by argument name alone", len(reaches), pluralBinds(len(reaches))),
        loggingcontract.Context{"command": instance.command, "globalBindReach": reaches},
    )
}

func pluralBinds(count int) string {
    if 1 == count {
        return "bind"
    }

    return "binds"
}

/* informationReportPrefixes are the report lines that state a fact of a scan that worked: the two counts, and the vendor trees stepped over, which cannot hold a service. The reach of a global bind is not among them: it is the only place an operator learns that one word bound many constructors, so its lines are journaled as one warning whose message carries the count and whose context carries them all. */
/* globalBindReportPrefix is the one line the journal aggregates rather than repeats; the text is the generator's */
const globalBindReportPrefix = "global bind "

var informationReportPrefixes = []string{
    "registered ",
    "skipped vendor directory: ",
}

func (instance *journalLineWriter) journalLine(line string) {
    if "" == line {
        return
    }

    if true == strings.HasPrefix(line, globalBindReportPrefix) {
        instance.globalBindReach = append(instance.globalBindReach, strings.TrimPrefix(line, globalBindReportPrefix))

        return
    }

    for _, prefix := range informationReportPrefixes {
        if true == strings.HasPrefix(line, prefix) {
            instance.logger.Info(line, loggingcontract.Context{"command": instance.command})

            return
        }
    }

    instance.logger.Warning(line, loggingcontract.Context{"command": instance.command})
}

/* journal answers the application's logger, resolved through the runtime so the scope's logger wins, and the emergency logger when the runtime carries none. The two doors below are copied from the openapi generate command, since sharing them would need an exported symbol. When an empty kernel.log_path makes the journal write to stdout, where the source goes, the emergency journal, stderr, carries the report. */
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

/* journalSharesStdout reads what the container reads when it builds the logger: an empty log path means the journal writes to stdout. The configuration is read tolerantly, answering false when it is absent or fails to resolve. A logger the application substituted is not readable here, so such an application gives the command --out. */
func journalSharesStdout(runtimeInstance runtimecontract.Runtime) bool {
    configuration, resolveErr := container.FromResolver[configcontract.Configuration](runtimeInstance.Container(), config.ServiceConfig)
    if nil != resolveErr || nil == configuration {
        return false
    }

    /* the kernel section is read with the same tolerance */
    kernelConfiguration := configuration.Kernel()
    if true == internal.IsNilInterface(kernelConfiguration) {
        return false
    }

    return "" == kernelConfiguration.LogPath()
}

/* writeReport prints what the generation covered and, more importantly, what it did not: a skipped constructor and an unmatched bind are both silent losses of coverage unless they are named. */
func (instance *GenerateCommand) writeReport(
    reportWriter io.Writer,
    commandContext clicontract.Context,
    report *GenerateReport,
) {
    fmt.Fprintf(reportWriter, "registered %d constructors\n", report.ConstructorCount)

    if 0 < report.ScopedConstructorCount {
        fmt.Fprintf(reportWriter, "registered %d scoped constructors\n", report.ScopedConstructorCount)
    }

    for _, skipped := range report.Skipped {
        fmt.Fprintf(
            reportWriter,
            "skipped %s (%s:%d): %s\n",
            skipped.Name,
            skipped.File,
            skipped.Line,
            skipped.Reason,
        )
    }

    /* vendor trees cannot contribute services, so naming them is opt-in */
    if true == commandContext.Bool("report-vendor") {
        for _, vendorDirectory := range report.SkippedVendorDirectories {
            fmt.Fprintf(reportWriter, "skipped vendor directory: %s\n", vendorDirectory)
        }
    }

    /* a build-excluded file holding a candidate is named on request, so a service built under a tag traces back to the tag to pass through --tags */
    if true == commandContext.Bool("report-excluded") {
        for _, excludedFile := range report.ExcludedFiles {
            fmt.Fprintf(reportWriter, "excluded by build constraints (holds a constructor candidate): %s\n", excludedFile)
        }
    }

    for _, unused := range report.UnusedBinds {
        fmt.Fprintf(reportWriter, "bind %s matched no constructor argument\n", unused)
    }

    for _, unused := range report.UnusedExcludes {
        fmt.Fprintf(reportWriter, "exclude %s matched no constructor\n", unused)
    }

    /* the command always hands over the running configuration, so this names the case where it declares nothing */
    if true == report.BindTargetsUnchecked {
        fmt.Fprint(reportWriter, "bind targets were not checked: the application declares no parameters\n")
    }

    reachedNames := make([]string, 0, len(report.GlobalBindReach))
    for argumentName := range report.GlobalBindReach {
        reachedNames = append(reachedNames, argumentName)
    }

    sort.Strings(reachedNames)

    for _, argumentName := range reachedNames {
        constructors := report.GlobalBindReach[argumentName]

        fmt.Fprintf(
            reportWriter,
            "global bind %s reaches %d %s: %s\n",
            argumentName,
            len(constructors),
            pluralConstructors(len(constructors)),
            strings.Join(constructors, ", "),
        )
    }
}

func pluralConstructors(count int) string {
    if 1 == count {
        return "constructor"
    }

    return "constructors"
}

/* splitBuildTags parses the comma-separated tag list. A build context carries plain tag identifiers, so a negation or a space-separated pair is refused here: it would match no file, and the tagged services would silently stay missing while strict reports success. */
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
