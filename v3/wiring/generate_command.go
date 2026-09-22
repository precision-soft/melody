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

/* Flags declares the quiet flag beside the command's own, defaulting to true as StandardFlags does: the generated source is the command's essential output and the run banner is decoration, and a command that declares no quiet flag keeps the banner it always had — around a source printed to stdout, that banner was the first bytes of the file. */
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

    /* the report goes on the writer when the source goes to a file, and into the journal when the writer IS the source: with --out empty the command has one writer and prints the source on it, so the report lines ahead of the package clause made the stdout mode print a file that does not compile — the same class the sibling openapi command closed for its warning */
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

    /* a generated file inside a scanned directory is read back by the next scan — with a package clause the surrounding sources do not carry, so the package stops compiling and the tool can no longer regenerate its way out; the constructor's own contract says the output package must not be a scanned one, and this is where it is enforceable. The containment is read on path components, the way the static file server reads its own: a directory named ..hidden inside the scanned one spells a relative path that starts with two dots without lying outside it, and a relative path the two paths cannot be related on (one absolute, one not) is refused rather than read as outside. Neither shape reaches here through the shipped callers — the project directory is always absolute and the scanner skips dot-directories — but the guard no longer depends on either fact. */
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

    /* the framework's own atomic writer, which this package can import directly: it lands the source through a temp file, a sync, a rename and a directory sync, so a write that dies partway leaves the previous file intact rather than a truncated Go source the compiler and the committed-file diff read as the generator's output */
    writeErr := internal.WriteFileAtomically(outputPath, []byte(source), "generated wiring file")
    if nil != writeErr {
        return writeErr
    }

    fmt.Fprintf(commandContext.Writer(), "wiring written to %s\n", outputPath)

    return nil
}



/* journalLineWriter carries the report into the journal, one record per line, in stdout mode, where the stream the report used to go to is the generated source. The count of what was registered is information; every other line of the report names coverage the wiring lost — a skipped constructor, a bind or an exclude that matched nothing, a file the build constraints left out, bind targets that went unchecked — and is journaled as a warning, because the application's journal drops what lies below its threshold and a deployment that journals to a file usually reads at warning: journaled as information, the whole report vanished from such a journal, and the losses with it. */
type journalLineWriter struct {
    logger  loggingcontract.Logger
    command string
    pending string

    /* globalBindReach collects the reach lines instead of journaling them one by one; flush turns them into the single record described above */
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

/* flush journals a last line written without its line end, which the line loop above keeps pending: every line writeReport prints ends with one today, so this is the door for the next line written without it */
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

/* informationReportPrefixes are the report lines that state a fact of a scan that WORKED rather than coverage it lost: the two counts, and the vendor trees the scan stepped over, which are opt-in and cannot hold a service, so stepping over one loses nothing. Everything else names something the wiring did not cover, which is what a journal read at the usual production threshold must keep.

   The reach of a global bind is not in this list, and the reason is the one this package's own test writes over the reach report: a global bind SILENTLY reaches every constructor declaring an argument of that name, and the line is the only place an operator learns that one word bound forty of them. Below the threshold it reaches nobody. What made it look like noise is that the generator emits one line per bound argument on every clean generation — so the repetition is what is cut, not the warning: the lines are collected and journaled as ONE record whose message carries the count and whose context carries them all. A clean generation raises one warning, whatever the number of binds, and it is the warning that says how wide they reach. */
/* globalBindReportPrefix is the one line the journal aggregates rather than repeats; the text is the generator's, written once here and once where the line is built */
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

/* journal answers the application's logger, resolved through the runtime so the scope's logger wins over the root's, and the emergency logger when the runtime carries none. The two doors below are the ones the sibling openapi generate command carries, copied rather than shared: the only home a shared door could have is an exported one, and this major publishes no new symbol. The application's journal is the wrong channel when it IS stdout — an empty kernel.log_path makes the container log to stdout, the writer the source goes to — so that configuration is read here and the emergency journal, stderr, carries the report for it. */
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

/* journalSharesStdout reads the fact the container reads when it builds the logger: an empty log path means the journal writes to stdout. The configuration is read tolerantly, the way the sibling openapi command reads it: a runtime without it, or one whose configuration fails to resolve, answers false and the application's logger is asked as before — a command that exists to print a document must not die on the door that only decides where its report goes. What this door cannot read is a logger the application substituted for the container's: the contract exposes no writer, so an application that journals to stdout through its own logger keeps the report out of the source only by giving the command --out. */
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

    /* vendor trees cannot contribute services, so naming them is opt-in: on a large project the list is noise, but a user wondering where a constructor went can ask for it */
    if true == commandContext.Bool("report-vendor") {
        for _, vendorDirectory := range report.SkippedVendorDirectories {
            fmt.Fprintf(reportWriter, "skipped vendor directory: %s\n", vendorDirectory)
        }
    }

    /* a build-excluded file holding a candidate is opt-in for the same reason: a foreign-GOOS variant is legitimate noise, but a user missing a service built under a tag can ask which files the scan left out and pass the tag through --tags */
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

    /* the generator's contract is to say when it could not check the bind targets; the command always hands over the running configuration, so this line names the degenerate case where that configuration declares nothing */
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
