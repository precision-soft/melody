package cron

import (
    "bytes"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    flagNameOutput               = "out"
    flagNameLogsDir              = "logs-dir"
    flagNameBinary               = "binary"
    flagNameDefaultUser          = "user"
    flagNameHeartbeatPath        = "heartbeat-path"
    flagNameHeartbeatCommand     = "heartbeat-command"
    flagNameHeartbeatDestination = "heartbeat-destination"
    flagNameTemplate             = "template"
    flagNamePrune                = "prune"
    flagNameImage                = "image"
    flagNameNamespace            = "namespace"
    flagNameRestartPolicy        = "restart-policy"
)

const heartbeatDestinationDefault = "default"

type GenerateCommand struct {
    configuration *Configuration
    templates     map[string]Template
}

func NewGenerateCommand(configuration *Configuration) *GenerateCommand {
    if nil == configuration {
        configuration = NewConfiguration()
    }

    command := &GenerateCommand{
        configuration: configuration,
        templates:     make(map[string]Template),
    }

    for _, builtinTemplate := range BuiltinTemplates() {
        command.RegisterTemplate(builtinTemplate)
    }

    return command
}

/* RegisterTemplate installs the template under its name, silently replacing an existing one: replacement by name is how an application overrides a builtin dialect, so a caller that must not replace anything checks the name first. */
func (instance *GenerateCommand) RegisterTemplate(template Template) {
    instance.templates[template.Name()] = template
}

func (instance *GenerateCommand) Name() string {
    return "melody:cron:generate"
}

func (instance *GenerateCommand) Description() string {
    return "Generate the crontab file from the cron Configuration registry"
}

func (instance *GenerateCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(output.StandardFlags(), instance.ownFlags())
}

/* ownFlags keeps the generator's flags apart from the standard set, since the framework rewrites -v/-vv into --verbosity for every command and a command without the standard flags fails with "flag provided but not defined". */
func (instance *GenerateCommand) ownFlags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  flagNameOutput,
            Usage: "path where the crontab will be written; overrides the melody.cron.destination_file parameter",
        },
        &clicontract.StringFlag{
            Name:  flagNameLogsDir,
            Usage: "directory used in the log redirection of generated entries; overrides the melody.cron.logs_dir parameter",
        },
        &clicontract.StringFlag{
            Name:  flagNameBinary,
            Usage: "absolute path of the binary referenced in crontab entries; overrides the melody.cron.binary parameter; defaults to the running binary when both are empty",
        },
        &clicontract.StringFlag{
            Name:  flagNameDefaultUser,
            Usage: "system user that runs each entry when the command does not specify one; overrides the melody.cron.user parameter",
        },
        &clicontract.StringFlag{
            Name:  flagNameHeartbeatPath,
            Usage: "if set, a 'touch <path>' entry runs every minute as the default user; overrides the melody.cron.heartbeat_path parameter and the melody.cron.heartbeat.enabled opt-in (which auto-derives <logs-dir>/heartbeat.crontab when neither is set)",
        },
        &clicontract.StringSliceFlag{
            Name:  flagNameHeartbeatCommand,
            Usage: "argv tokens for a custom heartbeat command (repeat to add tokens). When set, overrides --heartbeat-path",
        },
        &clicontract.StringSliceFlag{
            Name:  flagNameHeartbeatDestination,
            Usage: "restrict the heartbeat to the listed destinations (repeat for multiple). Use 'default' for the --out destination, an absolute path for an explicit file, or a relative path matched against dir(--out). When unset, the heartbeat goes to every destination",
        },
        &clicontract.StringFlag{
            Name:  flagNameTemplate,
            Usage: "name of the registered template that will render the entries; overrides the melody.cron.template parameter (default: crontab). Built-in templates: crontab, crontab-no-user, k8s",
        },
        &clicontract.BoolFlag{
            Name:  flagNamePrune,
            Usage: "empty the destinations in dir(--out) that this generator wrote earlier for this application and this run no longer produces, so an entry retired or moved between versions stops running. Only files carrying the current template's ownership line for this application are touched — a file another application wrote, or one written before the line named the application, is left alone — and destinations outside dir(--out) are never swept",
        },
        &clicontract.StringFlag{
            Name:  flagNameImage,
            Usage: "container image used by the k8s template; overrides the melody.cron.k8s.image parameter (required by the k8s template)",
        },
        &clicontract.StringFlag{
            Name:  flagNameNamespace,
            Usage: "namespace set on the k8s CronJob manifests; overrides the melody.cron.k8s.namespace parameter (omitted when empty)",
        },
        &clicontract.StringFlag{
            Name:  flagNameRestartPolicy,
            Usage: "restartPolicy set on the k8s CronJob pod template; overrides the melody.cron.k8s.restart_policy parameter (default: OnFailure)",
        },
    }
}

func (instance *GenerateCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    configuration, configurationErr := configurationFromRuntime(runtimeInstance)
    if nil != configurationErr {
        return configurationErr
    }

    return instance.runWithConfiguration(commandContext, configuration)
}

func (instance *GenerateCommand) resolveTemplate(name string) (Template, error) {
    template, ok := instance.templates[name]
    if false == ok {
        registered := make([]string, 0, len(instance.templates))
        for templateName := range instance.templates {
            registered = append(registered, templateName)
        }
        sort.Strings(registered)

        return nil, exception.NewError(
            fmt.Sprintf("cron: no template registered with name %q (registered: %s); call GenerateCommand.RegisterTemplate before running", name, strings.Join(registered, ", ")),
            exceptioncontract.Context{
                "requestedTemplate":   name,
                "registeredTemplates": registered,
            },
            ErrTemplateNotFound,
        )
    }

    return template, nil
}

type runOptions struct {
    template           Template
    outputPath         string
    logsDir            string
    binary             string
    defaultUserName    string
    heartbeatPath      string
    heartbeatCommand   []string
    heartbeatRequested []string
    heartbeatEnabled   bool
    prune              bool
    applicationName    string
    image              string
    namespace          string
    restartPolicy      string
}

func (instance *runOptions) rendersK8s() bool {
    return TemplateNameK8s == instance.template.Name()
}

/* reportWarning is a non-fatal finding the run wants in its report on both branches: a warning line in text mode, an envelope warning under --format=json. */
type reportWarning struct {
    code    string
    message string
}

func (instance *GenerateCommand) runWithConfiguration(
    commandContext clicontract.Context,
    configuration configcontract.Configuration,
) (runErr error) {
    startedAt := time.Now()
    option := output.NormalizeOption(output.ParseOptionFromCommand(commandContext))

    writes := ([]destinationWrite)(nil)
    pruned := ([]string)(nil)
    emptyMessage := ""
    warnings := ([]reportWarning)(nil)

    /* the report is a defer so that no failure path leaves the run without a document: the cli silences the command's own error line under --format=json, so an early return would hand a consumer an empty stream */
    defer func() {
        runErr = instance.reportWrites(commandContext, option, startedAt, writes, pruned, emptyMessage, warnings, runErr)
    }()

    options, resolveErr := instance.resolveRunOptions(commandContext, configuration)
    if nil != resolveErr {
        return resolveErr
    }

    /* the k8s template ignores the heartbeat, so an explicitly requested one is reported as dropped rather than swallowed or refused */
    if true == options.heartbeatEnabled && true == options.rendersK8s() {
        warnings = append(warnings, reportWarning{
            code:    "cron.heartbeatIgnored",
            message: "cron: heartbeat options are set but the k8s template ignores them; no liveness CronJob will be generated — model cluster liveness with a dedicated scheduled command instead",
        })
    }

    entries, collectErr := instance.collectScheduledEntries(options)
    if nil != collectErr {
        return collectErr
    }

    writeErr := (error)(nil)
    writes, pruned, emptyMessage, writeErr = instance.writeDestinations(options, entries)

    return writeErr
}

func (instance *GenerateCommand) resolveRunOptions(
    commandContext clicontract.Context,
    configuration configcontract.Configuration,
) (*runOptions, error) {
    options := &runOptions{}

    templateName := resolveDefault(commandContext, configuration, flagNameTemplate, ParameterTemplate)
    if "" == templateName {
        templateName = TemplateNameCrontab
    }

    template, templateLookupErr := instance.resolveTemplate(templateName)
    if nil != templateLookupErr {
        return nil, templateLookupErr
    }

    applicationName, applicationNameErr := applicationIdentity(configuration)
    if nil != applicationNameErr {
        return nil, applicationNameErr
    }
    options.applicationName = applicationName

    /* one template owned by this application renders and sweeps, so the ownership line every destination carries and the line the sweep asks for come from the same object */
    template = templateOwnedBy(template, applicationName)
    options.template = template

    /* the k8s template ignores the heartbeat, so no heartbeat path is derived for it; an explicitly requested one still flows through so the run can warn that it is dropped */
    isK8s := options.rendersK8s()

    /* a dialect that renders no user column never needs a user to place the heartbeat line */
    rendersUserColumn := templateRendersUserColumn(template)

    outputPath := resolveDefaultPath(commandContext, configuration, flagNameOutput, ParameterDestinationFile)
    if "" == outputPath {
        return nil, exception.NewError(
            "cron: no output path configured; set the cli flag or register the parameter (see RegisterDefaultParameters)",
            exceptioncontract.Context{
                "flag":      flagNameOutput,
                "parameter": ParameterDestinationFile,
            },
            ErrNoOutputPath,
        )
    }

    absoluteOutputPath, outputPathAbsErr := filepath.Abs(outputPath)
    if nil != outputPathAbsErr {
        return nil, exception.NewError(
            "cron: could not resolve absolute path for output",
            exceptioncontract.Context{"path": outputPath},
            outputPathAbsErr,
        )
    }
    options.outputPath = absoluteOutputPath

    logsDir := resolveDefaultPath(commandContext, configuration, flagNameLogsDir, ParameterLogsDir)
    if "" != logsDir {
        absoluteLogsDir, logsDirAbsErr := filepath.Abs(logsDir)
        if nil != logsDirAbsErr {
            return nil, exception.NewError(
                "cron: could not resolve absolute path for logs dir",
                exceptioncontract.Context{"path": logsDir},
                logsDirAbsErr,
            )
        }
        logsDir = absoluteLogsDir

        /* the logs directory is created before anything is rendered, because under system cron a shell whose redirection cannot create its file aborts the command; creating it is idempotent, so a run that fails later leaves it behind */
        if mkdirErr := os.MkdirAll(logsDir, 0o755); nil != mkdirErr {
            return nil, exception.NewError(
                "cron: could not create the logs directory",
                exceptioncontract.Context{"directory": logsDir},
                mkdirErr,
            )
        }
    }
    options.logsDir = logsDir

    /* a relative melody.cron.binary is anchored under the project like its three sibling paths, while a relative --binary keeps the shell's convention */
    options.binary = resolveDefaultPath(commandContext, configuration, flagNameBinary, ParameterBinary)
    options.defaultUserName = resolveDefault(commandContext, configuration, flagNameDefaultUser, ParameterUser)

    heartbeatPath := resolveDefaultPath(commandContext, configuration, flagNameHeartbeatPath, ParameterHeartbeatPath)
    if "" == heartbeatPath && "" != logsDir {
        autoEnabled, autoEnabledErr := isHeartbeatAutoEnabled(configuration)
        if nil != autoEnabledErr {
            return nil, autoEnabledErr
        }

        /* a malformed heartbeat opt-in fails under every template, while the derived path stays crontab-only so the k8s template never arms the dropped-heartbeat warning on a setting nobody made */
        if true == autoEnabled && false == isK8s {
            heartbeatPath = filepath.Join(logsDir, "heartbeat.crontab")
        }
    }
    if "" != heartbeatPath {
        absoluteHeartbeatPath, heartbeatPathAbsErr := filepath.Abs(heartbeatPath)
        if nil != heartbeatPathAbsErr {
            return nil, exception.NewError(
                "cron: could not resolve absolute path for heartbeat path",
                exceptioncontract.Context{"path": heartbeatPath},
                heartbeatPathAbsErr,
            )
        }
        heartbeatPath = absoluteHeartbeatPath
    }
    options.heartbeatPath = heartbeatPath

    options.heartbeatCommand = commandContext.StringSlice(flagNameHeartbeatCommand)
    options.heartbeatRequested = commandContext.StringSlice(flagNameHeartbeatDestination)
    options.heartbeatEnabled = 0 < len(options.heartbeatCommand) || "" != options.heartbeatPath
    options.prune = commandContext.Bool(flagNamePrune)

    options.image = resolveDefault(commandContext, configuration, flagNameImage, ParameterImage)
    options.namespace = resolveDefault(commandContext, configuration, flagNameNamespace, ParameterNamespace)
    options.restartPolicy = resolveDefault(commandContext, configuration, flagNameRestartPolicy, ParameterRestartPolicy)

    if true == options.heartbeatEnabled && true == rendersUserColumn && "" == options.defaultUserName {
        return nil, exception.NewError(
            "cron: heartbeat is configured but no user is set; pass --user, register the melody.cron.user parameter, or remove the heartbeat",
            exceptioncontract.Context{
                "flag":      flagNameDefaultUser,
                "parameter": ParameterUser,
                "template":  template.Name(),
            },
            ErrHeartbeatUserMissing,
        )
    }

    return options, nil
}

func (instance *GenerateCommand) collectScheduledEntries(options *runOptions) ([]Entry, error) {
    scheduledCommands := instance.configuration.Entries()
    if 0 == len(scheduledCommands) {
        return []Entry{}, nil
    }

    needsDefaultBinary := false
    for _, scheduled := range scheduledCommands {
        config := scheduled.Config
        if nil != config && 0 < len(config.Command) {
            continue
        }
        needsDefaultBinary = true
        break
    }

    binary := options.binary
    if true == needsDefaultBinary {
        resolvedBinary, binaryErr := resolveBinaryPath(binary)
        if nil != binaryErr {
            return nil, binaryErr
        }
        binary = resolvedBinary
    }
    options.binary = binary

    /* only the crontab template redirects output to a log file; the k8s template logs to stdout, so it does not inherit the logs-dir requirement */
    requiresLogPath := false == options.rendersK8s()

    entries := make([]Entry, 0, len(scheduledCommands))
    for _, scheduled := range scheduledCommands {
        config := scheduled.Config
        if nil == config {
            config = &EntryConfig{}
        }

        expanded, expandErr := expandEntriesForCommand(scheduled.CommandName, config, binary, options.defaultUserName, options.logsDir, requiresLogPath)
        if nil != expandErr {
            return nil, expandErr
        }

        entries = append(entries, expanded...)
    }

    return entries, nil
}

type destinationWrite struct {
    Destination   string `json:"destination"`
    Entries       int    `json:"entries"`
    HeartbeatOnly bool   `json:"heartbeatOnly"`
}

/* writeDestinations answers what it wrote, what there was to say about an empty run and what stopped it, and leaves the report to the caller's defer: destinations are written one by one with no rollback, so the writes before a failure tell the consumer what a partial run left on disk. */
func (instance *GenerateCommand) writeDestinations(
    options *runOptions,
    entries []Entry,
) ([]destinationWrite, []string, string, error) {
    /* the k8s namespace is a single global option, so resource-name collisions span every destination file; catch them across the whole set before any manifest is rendered or written */
    if true == options.rendersK8s() {
        if uniqueErr := ensureK8sNamesUnique(entries); nil != uniqueErr {
            return nil, nil, "", uniqueErr
        }
    }

    entriesByDestination, groupErr := groupEntriesByDestination(entries, options.outputPath)
    if nil != groupErr {
        return nil, nil, "", groupErr
    }

    /* an empty configuration still sweeps, since every destination an earlier configuration wrote is then stale; it stays a success */
    if 0 == len(entriesByDestination) && false == options.heartbeatEnabled {
        pruned, pruneErr := pruneStaleDestinations(options, nil)

        return nil, pruned, "the cron Configuration is empty and no --heartbeat-path or --heartbeat-command was provided; nothing to write", pruneErr
    }

    if 0 == len(entriesByDestination) && true == options.heartbeatEnabled {
        /* the k8s template renders no heartbeat, so an empty configuration leaves nothing to render, but the sweep still runs */
        if true == options.rendersK8s() {
            pruned, pruneErr := pruneStaleDestinations(options, nil)

            return nil, pruned, "the cron Configuration is empty and the k8s template does not emit heartbeat CronJobs; nothing to write", pruneErr
        }

        entriesByDestination[options.outputPath] = nil
    }

    destinationPaths := make([]string, 0, len(entriesByDestination))
    for destination := range entriesByDestination {
        destinationPaths = append(destinationPaths, destination)
    }
    sort.Strings(destinationPaths)

    /* the k8s template ignores heartbeat options, so the requested heartbeat destinations are dropped for it rather than resolved against the written destinations and refused */
    heartbeatRequested := options.heartbeatRequested
    if true == options.rendersK8s() {
        heartbeatRequested = nil
    }

    heartbeatDestinations, heartbeatDestinationsErr := resolveHeartbeatDestinations(
        heartbeatRequested,
        options.outputPath,
        destinationPaths,
    )
    if nil != heartbeatDestinationsErr {
        return nil, nil, "", heartbeatDestinationsErr
    }

    writes := make([]destinationWrite, 0, len(destinationPaths))
    for _, destination := range destinationPaths {
        destinationEntries := entriesByDestination[destination]

        renderOptions := RenderOptions{
            Image:         options.image,
            Namespace:     options.namespace,
            RestartPolicy: options.restartPolicy,
        }
        if true == options.heartbeatEnabled && true == heartbeatDestinations[destination] {
            renderOptions.HeartbeatUser = options.defaultUserName
            renderOptions.HeartbeatPath = options.heartbeatPath
            renderOptions.HeartbeatCommand = options.heartbeatCommand
        }

        content, renderErr := options.template.Render(destinationEntries, renderOptions)
        if nil != renderErr {
            return writes, nil, "", exception.NewError(
                fmt.Sprintf("cron: could not render %s content for %s: %s", options.template.Name(), destination, renderErr.Error()),
                exceptioncontract.Context{
                    "template":    options.template.Name(),
                    "destination": destination,
                },
                renderErr,
            )
        }

        if mkdirErr := os.MkdirAll(filepath.Dir(destination), 0o755); nil != mkdirErr {
            return writes, nil, "", exception.NewError(
                "cron: could not create the output directory",
                exceptioncontract.Context{"directory": filepath.Dir(destination)},
                mkdirErr,
            )
        }

        if writeErr := atomicWriteFile(destination, []byte(content), 0o644); nil != writeErr {
            return writes, nil, "", writeErr
        }

        writes = append(writes, destinationWrite{
            Destination:   destination,
            Entries:       len(destinationEntries),
            HeartbeatOnly: 0 == len(destinationEntries) && true == options.heartbeatEnabled,
        })
    }

    pruned, pruneErr := pruneStaleDestinations(options, writes)

    return writes, pruned, "", pruneErr
}

/* pruneStaleDestinations empties the destinations this generator wrote earlier that this run does not produce, so a retired or moved entry does not keep running under crond. Because emptying is irreversible it is opt-in, reads only the output directory without recursing or following an absolute destination, and empties only a file whose leading lines carry, as an exact line, the ownership line of the template generating now for this application; a run with no application name cannot sweep. Emptying renders the template with no entries, so the file keeps its header and marker and stays recognisable to the next run. */
func pruneStaleDestinations(options *runOptions, writes []destinationWrite) ([]string, error) {
    if false == options.prune {
        return nil, nil
    }

    /* without a name no line separates this application's destinations from another's, so the sweep is refused before the directory is read */
    if "" == options.applicationName {
        return nil, exception.NewError(
            "cron: --prune needs the name the application runs under to tell its own destinations from another application's, and the cli configuration carries none",
            exceptioncontract.Context{"flag": flagNamePrune},
            nil,
        )
    }

    ownedTemplate, isOwnedTemplate := options.template.(OwnedTemplate)
    if false == isOwnedTemplate {
        return nil, nil
    }

    marker := ownedTemplate.OwnershipMarker()
    if "" == strings.TrimSpace(marker) {
        return nil, nil
    }

    /* the sweep runs only on a line that names this application: a template the generator could not hand the name to, such as a custom dialect embedding a builtin, answers the bare prefix another application writes as well, so for it the destination is written and the sweep is refused, naming the line */
    if false == strings.HasSuffix(marker, " for "+options.applicationName) {
        return nil, exception.NewError(
            "cron: --prune sweeps only on an ownership line that names this application, and the template's line does not; give the dialect a line ending in \" for \" and the application's cli name, or generate without --prune",
            exceptioncontract.Context{"flag": flagNamePrune, "ownershipMarker": marker, "application": options.applicationName},
            nil,
        )
    }

    written := make(map[string]bool, len(writes))
    for _, write := range writes {
        written[write.Destination] = true
    }

    outputDirectory := filepath.Dir(options.outputPath)

    directoryEntries, readDirErr := os.ReadDir(outputDirectory)
    if nil != readDirErr {
        return nil, exception.NewError(
            "cron: could not read the output directory to prune it",
            exceptioncontract.Context{"directory": outputDirectory},
            readDirErr,
        )
    }

    emptyContent, renderErr := options.template.Render(nil, RenderOptions{})
    if nil != renderErr {
        return nil, exception.NewError(
            fmt.Sprintf("cron: could not render the empty %s content used to prune", options.template.Name()),
            exceptioncontract.Context{"template": options.template.Name()},
            renderErr,
        )
    }

    pruned := make([]string, 0)

    for _, directoryEntry := range directoryEntries {
        /* only a regular file is a candidate: opening a fifo with no writer blocks forever, and a device, socket or symlink is not something this generator wrote */
        if false == directoryEntry.Type().IsRegular() {
            continue
        }

        candidate := filepath.Join(outputDirectory, directoryEntry.Name())
        if true == written[candidate] {
            continue
        }

        owned, ownershipErr := fileCarriesOwnershipMarker(candidate, marker)
        if nil != ownershipErr {
            return pruned, ownershipErr
        }

        if false == owned {
            continue
        }

        if writeErr := atomicWriteFile(candidate, []byte(emptyContent), 0o644); nil != writeErr {
            return pruned, writeErr
        }

        pruned = append(pruned, candidate)
    }

    return pruned, nil
}

/* ownershipMarkerReadLimit bounds what is read to decide ownership: the marker rides in the header block every rendered destination opens with, and a file large enough to push it past this is not one this generator wrote. */
const ownershipMarkerReadLimit = 8 * 1024

/* ownershipMarkerLineLimit bounds where in that head the marker may stand: every builtin template renders it inside the leading comment block, so a file that merely quotes the marker further down is not emptied. */
const ownershipMarkerLineLimit = 10

/* fileCarriesOwnershipMarker recognises ownership by an exact marker line among the file's leading lines. Exactness keeps apart a marker that extends another, a custom dialect suffixing the builtin marker, one application's line and another's, and the bare prefix of a line that names no application. */
func fileCarriesOwnershipMarker(path string, marker string) (bool, error) {
    fileInstance, openErr := os.Open(path)
    if nil != openErr {
        return false, exception.NewError(
            "cron: could not read a candidate destination while pruning",
            exceptioncontract.Context{"destination": path},
            openErr,
        )
    }
    defer fileInstance.Close()

    head := make([]byte, ownershipMarkerReadLimit)

    read, readErr := io.ReadFull(fileInstance, head)
    if nil != readErr && io.ErrUnexpectedEOF != readErr && io.EOF != readErr {
        return false, exception.NewError(
            "cron: could not read a candidate destination while pruning",
            exceptioncontract.Context{"destination": path},
            readErr,
        )
    }

    remaining := head[:read]
    for lineIndex := 0; lineIndex < ownershipMarkerLineLimit; lineIndex++ {
        line, rest, _ := bytes.Cut(remaining, []byte("\n"))
        if marker == string(bytes.TrimSpace(line)) {
            return true, nil
        }

        if 0 == len(rest) {
            break
        }

        remaining = rest
    }

    return false, nil
}

/* reportWrites is the generator's one report door, reached from the run's defer on every path: text lines, or the single machine-readable document under --format=json carrying the failure beside what was already written. The summary is the command's whole result, so --quiet does not silence it. The run's failure stays the returned verdict; a rendering failure becomes one only when the run succeeded, and in text mode the failure travels alone for the cli entry point to print. */
func (instance *GenerateCommand) reportWrites(
    commandContext clicontract.Context,
    option output.Option,
    startedAt time.Time,
    writes []destinationWrite,
    pruned []string,
    emptyMessage string,
    warnings []reportWarning,
    runErr error,
) error {
    if true == output.IsJsonFormat(option.Format) {
        meta := output.NewMeta(instance.Name(), nil, option, startedAt, time.Since(startedAt), output.Version{})
        envelope := output.NewEnvelope(meta)

        if nil == writes {
            writes = []destinationWrite{}
        }

        /* pruned is an empty list rather than null when nothing was swept, so the field keeps its json type on every outcome */
        if nil == pruned {
            pruned = []string{}
        }

        envelope.Data = map[string]any{"writes": writes, "pruned": pruned}

        for _, warning := range warnings {
            envelope.Warnings = append(envelope.Warnings, output.NewWarning(warning.code, warning.message, nil))
        }

        if "" != emptyMessage {
            envelope.Warnings = append(envelope.Warnings, output.NewWarning("cron.nothingToWrite", emptyMessage, nil))
        }

        if nil != runErr {
            /* the envelope carries the failure's details and cause, and details stays an object when the failure carries no context, so the field keeps its json type on every failure */
            envelope.SetError(
                "cron.generateFailed",
                "the cron manifest generation failed",
                errorDetailsOf(runErr),
                errorCauseOf(runErr),
            )
        }

        renderErr := output.Render(commandContext.Writer(), envelope, option)
        if nil != runErr {
            return runErr
        }

        return renderErr
    }

    for _, warning := range warnings {
        _, _ = fmt.Fprintln(commandContext.Writer(), warning.message)
    }

    /* a run that fails part way still prints what it wrote and pruned, since emptying is irreversible and the operator needs to know which manifests were blanked */
    if nil != runErr {
        printDestinationWrites(commandContext, writes)
        printPrunedDestinations(commandContext, pruned)

        return runErr
    }

    if "" != emptyMessage {
        _, _ = fmt.Fprintln(commandContext.Writer(), emptyMessage)

        printPrunedDestinations(commandContext, pruned)

        return nil
    }

    printDestinationWrites(commandContext, writes)

    printPrunedDestinations(commandContext, pruned)

    return nil
}

func printDestinationWrites(commandContext clicontract.Context, writes []destinationWrite) {
    for _, write := range writes {
        if true == write.HeartbeatOnly {
            _, _ = fmt.Fprintf(commandContext.Writer(), "wrote heartbeat-only crontab to %s\n", write.Destination)
        } else {
            _, _ = fmt.Fprintf(commandContext.Writer(), "wrote %d entries to %s\n", write.Entries, write.Destination)
        }
    }
}

func printPrunedDestinations(commandContext clicontract.Context, pruned []string) {
    for _, destination := range pruned {
        _, _ = fmt.Fprintf(commandContext.Writer(), "pruned %s\n", destination)
    }
}

/* atomicWriteFile writes the content to a temporary file beside the destination and renames it into place, removing the temporary file on every failure it sees. The mode applies to a new destination; an existing one keeps its permission bits, without setuid, setgid and sticky, so a crontab narrowed to 0600 is not widened. A process killed before the rename leaves the temporary file carrying the ownership marker, which a later --prune empties and reports. */
func atomicWriteFile(destination string, content []byte, mode os.FileMode) error {
    tmpFile, createErr := os.CreateTemp(filepath.Dir(destination), filepath.Base(destination)+".*.tmp")
    if nil != createErr {
        return exception.NewError(
            "cron: could not create temporary file next to destination",
            exceptioncontract.Context{"destination": destination},
            createErr,
        )
    }

    tmpPath := tmpFile.Name()
    renamed := false
    defer func() {
        if false == renamed {
            _ = os.Remove(tmpPath)
        }
    }()

    if _, writeErr := tmpFile.Write(content); nil != writeErr {
        _ = tmpFile.Close()
        return exception.NewError(
            "cron: could not write temporary file",
            exceptioncontract.Context{"path": tmpPath},
            writeErr,
        )
    }

    if syncErr := tmpFile.Sync(); nil != syncErr {
        _ = tmpFile.Close()
        return exception.NewError(
            "cron: could not fsync temporary file",
            exceptioncontract.Context{"path": tmpPath},
            syncErr,
        )
    }

    if closeErr := tmpFile.Close(); nil != closeErr {
        return exception.NewError(
            "cron: could not close temporary file",
            exceptioncontract.Context{"path": tmpPath},
            closeErr,
        )
    }

    if chmodErr := os.Chmod(tmpPath, destinationFileMode(destination, mode)); nil != chmodErr {
        return exception.NewError(
            "cron: could not chmod temporary file",
            exceptioncontract.Context{
                "path": tmpPath,
                "mode": fmt.Sprintf("%#o", mode),
            },
            chmodErr,
        )
    }

    if renameErr := os.Rename(tmpPath, destination); nil != renameErr {
        return exception.NewError(
            "cron: could not rename temporary file over destination",
            exceptioncontract.Context{
                "source":      tmpPath,
                "destination": destination,
            },
            renameErr,
        )
    }

    renamed = true

    if dirSyncErr := syncDir(filepath.Dir(destination)); nil != dirSyncErr {
        return dirSyncErr
    }

    return nil
}

/* destinationFileMode answers the permission bits the destination already carries, without setuid, setgid and sticky, or the caller's mode for a new file. */
func destinationFileMode(destination string, newFileMode os.FileMode) os.FileMode {
    info, statErr := os.Stat(destination)
    if nil != statErr {
        return newFileMode
    }

    return info.Mode().Perm()
}

func syncDir(path string) error {
    dir, openErr := os.Open(path)
    if nil != openErr {
        return exception.NewError(
            "cron: could not open destination directory for fsync",
            exceptioncontract.Context{"directory": path},
            openErr,
        )
    }

    syncErr := dir.Sync()
    closeErr := dir.Close()

    if nil != syncErr && nil != closeErr {
        return exception.NewError(
            "cron: fsync and close failed on destination directory",
            exceptioncontract.Context{"directory": path},
            errors.Join(syncErr, closeErr),
        )
    }

    if nil != syncErr {
        return exception.NewError(
            "cron: could not fsync destination directory",
            exceptioncontract.Context{"directory": path},
            syncErr,
        )
    }

    if nil != closeErr {
        return exception.NewError(
            "cron: could not close destination directory after fsync",
            exceptioncontract.Context{"directory": path},
            closeErr,
        )
    }

    return nil
}

func resolveBinaryPath(explicit string) (string, error) {
    if "" == explicit {
        resolved, resolveErr := os.Executable()
        if nil != resolveErr {
            return "", exception.NewError(
                "cron: could not resolve the running executable path",
                nil,
                resolveErr,
            )
        }

        explicit = resolved
    }

    absolute, absErr := filepath.Abs(explicit)
    if nil != absErr {
        return "", exception.NewError(
            "cron: could not resolve absolute path for binary",
            exceptioncontract.Context{"path": explicit},
            absErr,
        )
    }

    return absolute, nil
}

func groupEntriesByDestination(entries []Entry, defaultDestination string) (map[string][]Entry, error) {
    grouped := make(map[string][]Entry)
    defaultDir := filepath.Dir(defaultDestination)

    for _, entry := range entries {
        destination, resolveErr := resolveEntryDestination(entry.DestinationFile, defaultDestination, defaultDir)
        if nil != resolveErr {
            return nil, resolveErr
        }

        grouped[destination] = append(grouped[destination], entry)
    }

    return grouped, nil
}

func resolveEntryDestination(entryDestination string, defaultDestination string, defaultDir string) (string, error) {
    if "" == entryDestination {
        return defaultDestination, nil
    }

    if true == filepath.IsAbs(entryDestination) {
        return filepath.Clean(entryDestination), nil
    }

    joined := filepath.Join(defaultDir, entryDestination)
    cleanedDir := filepath.Clean(defaultDir)

    if false == isWithinDir(joined, cleanedDir) {
        return "", exception.NewError(
            "cron: EntryConfig.DestinationFile escapes the default destination directory; use an absolute path if the destination must live elsewhere",
            exceptioncontract.Context{
                "destinationFile":  entryDestination,
                "resolvedPath":     joined,
                "defaultDirectory": cleanedDir,
            },
            ErrDestinationEscape,
        )
    }

    return joined, nil
}

/* isWithinDir answers on the cleaned names deliberately: it refuses a DestinationFile that walks out of dir(--out) with "..", while a symbolic link inside the directory is the operator's layout and is allowed, as an absolute path is. */
func isWithinDir(candidate string, parent string) bool {
    if candidate == parent {
        return true
    }

    parentWithSep := parent
    if false == strings.HasSuffix(parentWithSep, string(filepath.Separator)) {
        parentWithSep += string(filepath.Separator)
    }

    return strings.HasPrefix(candidate, parentWithSep)
}

func resolveHeartbeatDestinations(
    requested []string,
    outputPath string,
    destinationPaths []string,
) (map[string]bool, error) {
    selected := make(map[string]bool, len(destinationPaths))

    if 0 == len(requested) {
        for _, destination := range destinationPaths {
            selected[destination] = true
        }

        return selected, nil
    }

    destinationSet := make(map[string]bool, len(destinationPaths))
    for _, destination := range destinationPaths {
        destinationSet[destination] = true
    }

    defaultDir := filepath.Dir(outputPath)

    for _, value := range requested {
        if heartbeatDestinationDefault == value {
            if false == destinationSet[outputPath] {
                return nil, exception.NewError(
                    "cron: --heartbeat-destination=default requested but the default destination has no entries and would not be written",
                    exceptioncontract.Context{"defaultDestination": outputPath},
                    ErrHeartbeatDestinationDefaultMissing,
                )
            }

            selected[outputPath] = true
            continue
        }

        candidate := value
        if false == filepath.IsAbs(candidate) {
            candidate = filepath.Join(defaultDir, candidate)
        } else {
            candidate = filepath.Clean(candidate)
        }

        if false == destinationSet[candidate] {
            return nil, exception.NewError(
                fmt.Sprintf("cron: --%s=%q resolves to %q, which is not among the destinations being written (%s)", flagNameHeartbeatDestination, value, candidate, strings.Join(destinationPaths, ", ")),
                exceptioncontract.Context{
                    "requested":         value,
                    "resolved":          candidate,
                    "validDestinations": destinationPaths,
                },
                ErrHeartbeatDestinationUnmatched,
            )
        }

        selected[candidate] = true
    }

    return selected, nil
}

func expandEntriesForCommand(
    commandName string,
    config *EntryConfig,
    binary string,
    defaultUserName string,
    logsDir string,
    requiresLogPath bool,
) ([]Entry, error) {
    user := config.User
    if "" == user {
        user = defaultUserName
    }

    instances := config.Instances
    if instances < 1 {
        instances = 1
    }

    entries := make([]Entry, 0, instances)
    for index := 1; index <= instances; index++ {
        logPath, logPathErr := resolveEntryLogPath(commandName, config, logsDir, instances, index, requiresLogPath)
        if nil != logPathErr {
            return nil, logPathErr
        }

        /* every entry carries its own schedule copy: Render is userland and Schedule.Defaults mutates in place, so a template calling it would otherwise rewrite the schedule of every sibling entry */
        entry := Entry{
            Name:            commandName,
            User:            user,
            Schedule:        copySchedule(config.Schedule),
            Command:         config.Command,
            LogPath:         logPath,
            DestinationFile: config.DestinationFile,
            InstanceIndex:   index,
            InstanceCount:   instances,
        }

        if 0 == len(config.Command) {
            args := []string{commandName}
            if 1 < instances {
                args = append(args,
                    fmt.Sprintf("--max-instances=%d", instances),
                    fmt.Sprintf("--instance-index=%d", index),
                )
            }

            /* the entry's own arguments come after the instance flags, so the manifest line runs the command line the in-process runner hands the child */
            args = append(args, config.Arguments...)

            entry.Binary = binary
            entry.Args = args
        }

        entries = append(entries, entry)
    }

    return entries, nil
}

func resolveEntryLogPath(
    commandName string,
    config *EntryConfig,
    logsDir string,
    instances int,
    index int,
    requiresLogPath bool,
) (string, error) {
    if true == config.LogDisabled {
        return "", nil
    }

    /* the active template does not redirect output to a log file (k8s), so there is no log path to resolve and no logs-dir to demand */
    if false == requiresLogPath {
        return "", nil
    }

    if "" == logsDir {
        return "", exception.NewError(
            "cron: command wants log redirection but no logs-dir is configured; set --logs-dir, register the melody.cron.logs_dir parameter, or set EntryConfig.LogDisabled=true",
            exceptioncontract.Context{
                "command":   commandName,
                "flag":      flagNameLogsDir,
                "parameter": ParameterLogsDir,
            },
            ErrNoLogsDir,
        )
    }

    logFileName := config.LogFileName
    if "" == logFileName {
        if true == config.LogFileNameRaw {
            logFileName = rawLogFileName(commandName) + ".log"
        } else {
            logFileName = sanitizeLogFileName(commandName) + ".log"
        }
    }

    if 1 < instances {
        base, extension := splitLogFileExtension(logFileName)
        logFileName = fmt.Sprintf("%s-%d%s", base, index, extension)
    }

    joined := filepath.Join(logsDir, logFileName)
    cleanedLogsDir := filepath.Clean(logsDir)

    if false == isWithinDir(joined, cleanedLogsDir) {
        return "", exception.NewError(
            "cron: EntryConfig.LogFileName resolves to a path that escapes the logs directory; use a file name that stays within the logs dir",
            exceptioncontract.Context{
                "command":      commandName,
                "logFileName":  config.LogFileName,
                "resolvedPath": joined,
                "logsDir":      cleanedLogsDir,
            },
            ErrDestinationEscape,
        )
    }

    /* a LogFileName with a subdirectory stays within the logs dir but nothing else creates that subdirectory, and under system cron the shell aborts the command when the >> redirection cannot create its file */
    if mkdirErr := os.MkdirAll(filepath.Dir(joined), 0o755); nil != mkdirErr {
        return "", exception.NewError(
            "cron: could not create the log file directory",
            exceptioncontract.Context{
                "command":   commandName,
                "directory": filepath.Dir(joined),
            },
            mkdirErr,
        )
    }

    return joined, nil
}

/* configurationFromRuntime resolves through the run's scope with the container as the fallback, so a scope-level substitution of the configuration is honoured. */
func configurationFromRuntime(runtimeInstance runtimecontract.Runtime) (configcontract.Configuration, error) {
    configuration, fromRuntimeErr := runtime.FromRuntime[configcontract.Configuration](runtimeInstance, melodyconfig.ServiceConfig)
    if nil != fromRuntimeErr {
        return nil, exception.NewError(
            "cron: could not resolve the configuration service from the runtime",
            exceptioncontract.Context{"service": melodyconfig.ServiceConfig},
            fromRuntimeErr,
        )
    }

    return configuration, nil
}

/* applicationIdentity answers the application's cli name, which the ownership line names. A configuration without a cli configuration answers the empty name, which the sweep refuses, and a name spanning lines is refused, since the ownership line has to be one line to be read back. */
func applicationIdentity(configuration configcontract.Configuration) (string, error) {
    cliConfiguration := configuration.Cli()
    if true == isNilInterface(cliConfiguration) {
        return "", nil
    }

    applicationName := strings.TrimSpace(cliConfiguration.Name())
    if true == strings.ContainsAny(applicationName, "\r\n") {
        return "", exception.NewError(
            "cron: the name the application runs under spans lines, so it cannot open the ownership line of a generated destination; configure a single-line cli name",
            exceptioncontract.Context{"applicationName": applicationName},
            nil,
        )
    }

    return applicationName, nil
}

/* templateOwnedBy hands the application's name to a template that can carry it and answers the copy that renders that application's ownership line; a custom dialect is used as it is, with whatever line it declares, and an empty name leaves every template unowned. The copy is derived only for the package's own dialects by concrete type, so a wrapper that embeds a builtin keeps its own rendering; the line it answers is the builtin's bare prefix, on which pruneStaleDestinations refuses to sweep. */
func templateOwnedBy(template Template, applicationName string) Template {
    if "" == applicationName {
        return template
    }

    switch ownedTemplate := template.(type) {
    case *CrontabTemplate:
        return ownedTemplate.ownedBy(applicationName)
    case *K8sTemplate:
        return ownedTemplate.ownedBy(applicationName)
    }

    return template
}

func resolveDefault(
    commandContext clicontract.Context,
    configuration configcontract.Configuration,
    flagName string,
    parameterName string,
) string {
    if value, typed := typedFlagValue(commandContext, flagName); true == typed {
        return value
    }

    return parameterValue(configuration, parameterName)
}

/* resolveDefaultPath is resolveDefault for a path: a relative path from a parameter is anchored at the project directory, as melody anchors MELODY_LOG_PATH, kernel.logs_dir and kernel.cache_dir, while one typed as a cli flag stays relative to the working directory the shell resolves it against. */
func resolveDefaultPath(
    commandContext clicontract.Context,
    configuration configcontract.Configuration,
    flagName string,
    parameterName string,
) string {
    if value, typed := typedFlagValue(commandContext, flagName); true == typed {
        return value
    }

    return anchorConfiguredPath(parameterValue(configuration, parameterName), configuration)
}

/* typedFlagValue answers the value of a flag the operator typed; a flag typed empty is not an answer, so the parameter behind it still is */
func typedFlagValue(commandContext clicontract.Context, flagName string) (string, bool) {
    if false == commandContext.IsSet(flagName) {
        return "", false
    }

    value := commandContext.String(flagName)

    return value, "" != value
}

func parameterValue(configuration configcontract.Configuration, parameterName string) string {
    if nil == configuration {
        return ""
    }

    parameter := configuration.Get(parameterName)
    if nil == parameter {
        return ""
    }

    return parameter.String()
}

/* anchorConfiguredPath carries locally the rule application/bootstrap.go applies to every other runtime path, which is unexported and in another module. A configuration that names no project directory keeps the working-directory anchoring rather than failing boot. */
func anchorConfiguredPath(value string, configuration configcontract.Configuration) string {
    if "" == value {
        return value
    }

    if true == filepath.IsAbs(value) {
        return value
    }

    kernelConfiguration := configuration.Kernel()
    if nil == kernelConfiguration {
        return value
    }

    projectDirectory := kernelConfiguration.ProjectDir()
    if "" == projectDirectory {
        return value
    }

    return filepath.Join(projectDirectory, value)
}

/* isHeartbeatAutoEnabled separates an unset opt-in from a malformed one, so a misspelt value is refused instead of generating a crontab without the liveness line the operator asked for. */
func isHeartbeatAutoEnabled(configuration configcontract.Configuration) (bool, error) {
    if nil == configuration {
        return false, nil
    }

    parameter := configuration.Get(ParameterHeartbeatAutoEnabled)
    if nil == parameter {
        return false, nil
    }

    enabled, parameterBoolErr := parameter.Bool()
    if nil != parameterBoolErr {
        return false, exception.NewError(
            "cron: the heartbeat opt-in parameter does not hold a boolean",
            exceptioncontract.Context{
                "parameter": ParameterHeartbeatAutoEnabled,
            },
            parameterBoolErr,
        )
    }

    return enabled, nil
}

var _ clicontract.Command = (*GenerateCommand)(nil)

/* errorDetailsOf renders the failure's own context as the json envelope's details, an empty object rather than null when it carries none, so the field keeps its json type. */
func errorDetailsOf(runErr error) map[string]any {
    details := map[string]any{}

    var provider exceptioncontract.ContextProvider
    /* a typed-nil link in the chain satisfies As and passes a plain nil comparison, and Context() on the nil receiver would panic inside the report */
    if true == errors.As(runErr, &provider) && false == isNilInterface(provider) {
        for key, value := range provider.Context() {
            details[key] = value
        }
    }

    return details
}

/* errorCauseOf answers the failure's text with the whole chain beneath it, starting at the failure itself because the envelope's message is a fixed label and the failure's own sentence lives in the cause. */
func errorCauseOf(runErr error) *output.ErrorCause {
    causeChain := exception.BuildCauseChain(runErr, 8)
    if 0 == len(causeChain) {
        return nil
    }

    return output.NewErrorCause(causeChain[0], map[string]any{"chain": causeChain})
}
