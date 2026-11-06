package cron

import (
    "bytes"
    "errors"
    "fmt"
    "io"
    "os"
    "os/user"
    "path/filepath"
    "sort"
    "strconv"
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

/* RegisterTemplate silently replaces an existing template of that name: this is how a builtin dialect is overridden. */
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

/* ownFlags stays apart from the standard set: a command without those fails with "flag provided but not defined" */
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
    commandContext *clicontract.CommandContext,
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
    logDirectoryOwnership *logDirectoryOwnership
    template              Template
    outputPath            string
    logsDir               string
    binary                string
    defaultUserName       string
    heartbeatPath         string
    heartbeatCommand      []string
    heartbeatRequested    []string
    heartbeatEnabled      bool
    prune                 bool
    applicationName       string
    image                 string
    namespace             string
    restartPolicy         string
}

func (instance *runOptions) rendersK8s() bool {
    return TemplateNameK8s == instance.template.Name()
}

type reportWarning struct {
    code    string
    message string
}

func (instance *GenerateCommand) runWithConfiguration(
    commandContext *clicontract.CommandContext,
    configuration configcontract.Configuration,
) (runErr error) {
    startedAt := time.Now()
    option := output.NormalizeOption(output.ParseOptionFromCommand(commandContext))

    writes := ([]destinationWrite)(nil)
    pruned := ([]string)(nil)
    emptyMessage := ""
    warnings := ([]reportWarning)(nil)

    /* a defer so no failure path leaves the run without a document: the cli silences the error line under --format=json */
    defer func() {
        runErr = instance.reportWrites(commandContext, option, startedAt, writes, pruned, emptyMessage, warnings, runErr)
    }()

    options, resolveErr := instance.resolveRunOptions(commandContext, configuration)
    if nil != resolveErr {
        return resolveErr
    }

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

    for _, unowned := range options.logDirectoryOwnership.unowned {
        warnings = append(warnings, reportWarning{
            code:    "cron.logDirectoryOwnerUnknown",
            message: unowned.warningMessage(),
        })
    }

    writeErr := (error)(nil)
    writes, pruned, emptyMessage, writeErr = instance.writeDestinations(options, entries)

    return writeErr
}

func (instance *GenerateCommand) resolveRunOptions(
    commandContext *clicontract.CommandContext,
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

    /* one template renders and sweeps, so the line every destination carries and the line the sweep asks for are one */
    template = templateOwnedBy(template, applicationName)
    options.template = template

    isK8s := options.rendersK8s()

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

        if mkdirErr := os.MkdirAll(logsDir, 0o755); nil != mkdirErr {
            return nil, exception.NewError(
                "cron: could not create the logs directory",
                exceptioncontract.Context{"directory": logsDir},
                mkdirErr,
            )
        }
    }
    options.logsDir = logsDir

    /* a relative melody.cron.binary is anchored under the project, while a relative --binary keeps the shell's convention */
    options.binary = resolveDefaultPath(commandContext, configuration, flagNameBinary, ParameterBinary)
    options.defaultUserName = resolveDefault(commandContext, configuration, flagNameDefaultUser, ParameterUser)

    heartbeatPath := resolveDefaultPath(commandContext, configuration, flagNameHeartbeatPath, ParameterHeartbeatPath)
    if "" == heartbeatPath && "" != logsDir {
        autoEnabled, autoEnabledErr := isHeartbeatAutoEnabled(configuration)
        if nil != autoEnabledErr {
            return nil, autoEnabledErr
        }

        /* the derived path stays crontab-only, so k8s never arms the dropped-heartbeat warning on a setting nobody made */
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
    options.logDirectoryOwnership = &logDirectoryOwnership{rendersUserColumn: templateRendersUserColumn(options.template)}

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

    requiresLogPath := false == options.rendersK8s()

    entries := make([]Entry, 0, len(scheduledCommands))
    for _, scheduled := range scheduledCommands {
        config := scheduled.Config
        if nil == config {
            config = &EntryConfig{}
        }

        expanded, expandErr := expandEntriesForCommand(scheduled.CommandName, config, binary, options.defaultUserName, options.logsDir, requiresLogPath, options.logDirectoryOwnership)
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

func (instance *GenerateCommand) writeDestinations(
    options *runOptions,
    entries []Entry,
) ([]destinationWrite, []string, string, error) {
    /* the k8s namespace is global, so resource-name collisions span every destination: catch them before any is rendered */
    if true == options.rendersK8s() {
        if uniqueErr := ensureK8sNamesUnique(entries); nil != uniqueErr {
            return nil, nil, "", uniqueErr
        }
    }

    entriesByDestination, groupErr := groupEntriesByDestination(entries, options.outputPath)
    if nil != groupErr {
        return nil, nil, "", groupErr
    }

    /* an empty configuration still sweeps: every destination an earlier configuration wrote is then stale */
    if 0 == len(entriesByDestination) && false == options.heartbeatEnabled {
        pruned, pruneErr := pruneStaleDestinations(options, nil)

        return nil, pruned, "the cron Configuration is empty and no --heartbeat-path or --heartbeat-command was provided; nothing to write", pruneErr
    }

    if 0 == len(entriesByDestination) && true == options.heartbeatEnabled {
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

    /* the k8s template ignores heartbeat options, so requested destinations are dropped here rather than refused */
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

/* pruneStaleDestinations is opt-in, emptying being irreversible, and empties only a file with this application's line */
func pruneStaleDestinations(options *runOptions, writes []destinationWrite) ([]string, error) {
    if false == options.prune {
        return nil, nil
    }

    /* without a name no line separates this application's destinations from another's */
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

    /* the sweep runs only on a line naming this application: the bare prefix is one another application writes as well */
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
        /* only a regular file is a candidate: opening a fifo with no writer blocks forever */
        if false == directoryEntry.Type().IsRegular() {
            continue
        }

        candidate := filepath.Join(outputDirectory, directoryEntry.Name())
        if true == written[candidate] {
            continue
        }

        /* a young temporary file may belong to a run in flight, which would rename the emptied file over its live destination */
        if true == isGeneratorTemporaryFile(directoryEntry.Name()) {
            info, infoErr := directoryEntry.Info()
            if nil != infoErr || time.Since(info.ModTime()) < pruneTemporaryFileGrace {
                continue
            }
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

const pruneTemporaryFileGrace = 5 * time.Minute

func isGeneratorTemporaryFile(name string) bool {
    return true == strings.HasPrefix(name, ".melody-cron-") && true == strings.HasSuffix(name, ".tmp")
}

const ownershipMarkerReadLimit = 8 * 1024

/* ownershipMarkerLineLimit: a file that merely quotes the marker further down is not emptied */
const ownershipMarkerLineLimit = 10

/* ownership is an exact marker line: it keeps apart a marker extending another and the lines of two applications */
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

/* reportWrites ignores --quiet: the summary is the command's whole result */
func (instance *GenerateCommand) reportWrites(
    commandContext *clicontract.CommandContext,
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

        /* an empty list rather than null, so the field keeps its json type */
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
            envelope.SetError(
                "cron.generateFailed",
                "the cron manifest generation failed",
                errorDetailsOf(runErr),
                errorCauseOf(runErr),
            )
        }

        renderErr := output.Render(commandContext.Writer, envelope, option)
        if nil != runErr {
            return runErr
        }

        return renderErr
    }

    for _, warning := range warnings {
        _, _ = fmt.Fprintln(commandContext.Writer, warning.message)
    }

    /* a run that fails part way still prints what it wrote and pruned: emptying is irreversible */
    if nil != runErr {
        printDestinationWrites(commandContext, writes)
        printPrunedDestinations(commandContext, pruned)

        return runErr
    }

    if "" != emptyMessage {
        _, _ = fmt.Fprintln(commandContext.Writer, emptyMessage)

        printPrunedDestinations(commandContext, pruned)

        return nil
    }

    printDestinationWrites(commandContext, writes)

    printPrunedDestinations(commandContext, pruned)

    return nil
}

func printDestinationWrites(commandContext *clicontract.CommandContext, writes []destinationWrite) {
    for _, write := range writes {
        if true == write.HeartbeatOnly {
            _, _ = fmt.Fprintf(commandContext.Writer, "wrote heartbeat-only crontab to %s\n", write.Destination)
        } else {
            _, _ = fmt.Fprintf(commandContext.Writer, "wrote %d entries to %s\n", write.Entries, write.Destination)
        }
    }
}

func printPrunedDestinations(commandContext *clicontract.CommandContext, pruned []string) {
    for _, destination := range pruned {
        _, _ = fmt.Fprintf(commandContext.Writer, "pruned %s\n", destination)
    }
}

/* atomicWriteFile keeps an existing destination's permission bits, so a crontab narrowed to 0600 is not widened */
func atomicWriteFile(destination string, content []byte, mode os.FileMode) error {
    /* the temp name omits the destination's basename, which may fill the 255 bytes of a path component on its own */
    tmpFile, createErr := os.CreateTemp(filepath.Dir(destination), ".melody-cron-*.tmp")
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

    appliedMode := destinationFileMode(destination, mode)
    if chmodErr := os.Chmod(tmpPath, appliedMode); nil != chmodErr {
        return exception.NewError(
            "cron: could not chmod temporary file",
            exceptioncontract.Context{
                "path":        tmpPath,
                "mode":        fmt.Sprintf("%#o", appliedMode),
                "newFileMode": fmt.Sprintf("%#o", mode),
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

/* isWithinDir answers on the cleaned names deliberately: a symbolic link inside the directory is the operator's layout */
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
    ownership *logDirectoryOwnership,
) ([]Entry, error) {
    user := config.User
    if "" == user {
        user = defaultUserName
    }

    instances := config.Instances
    if instances < 1 {
        instances = 1
    }

    logDirectoryOwner := ""
    if nil != ownership && true == ownership.rendersUserColumn {
        logDirectoryOwner = user
    }

    entries := make([]Entry, 0, instances)
    for index := 1; index <= instances; index++ {
        logPath, unownedDirectory, logPathErr := resolveEntryLogPath(commandName, config, logsDir, instances, index, requiresLogPath, logDirectoryOwner)
        if nil != logPathErr {
            return nil, logPathErr
        }

        if "" != unownedDirectory && nil != ownership {
            ownership.unowned = append(ownership.unowned, unownedLogDirectory{command: commandName, directory: unownedDirectory, user: logDirectoryOwner})
        }

        /* every entry carries its own schedule copy: Render is userland and Schedule.Defaults mutates in place */
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

            /* the entry's arguments come after the instance flags: the manifest line runs what the in-process runner hands the child */
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
    logDirectoryOwner string,
) (logPath string, unownedDirectory string, err error) {
    if true == config.LogDisabled {
        return "", "", nil
    }

    if false == requiresLogPath {
        return "", "", nil
    }

    if "" == logsDir {
        return "", "", exception.NewError(
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
        base, extension := splitLogFileExtension(filepath.Base(logFileName))
        logFileName = filepath.Join(filepath.Dir(logFileName), fmt.Sprintf("%s-%d%s", base, index, extension))
    }

    joined := filepath.Join(logsDir, logFileName)
    cleanedLogsDir := filepath.Clean(logsDir)

    if false == isWithinDir(joined, cleanedLogsDir) {
        return "", "", exception.NewError(
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

    /* nothing else creates a LogFileName subdirectory, and system cron aborts when the >> redirection cannot create its file */
    missingDirectories := missingDirectoriesBelow(filepath.Dir(joined), cleanedLogsDir)

    if mkdirErr := os.MkdirAll(filepath.Dir(joined), 0o755); nil != mkdirErr {
        return "", "", exception.NewError(
            "cron: could not create the log file directory",
            exceptioncontract.Context{
                "command":   commandName,
                "directory": filepath.Dir(joined),
            },
            mkdirErr,
        )
    }

    unownedDirectory, chownErr := chownCreatedLogDirectories(commandName, missingDirectories, logDirectoryOwner)
    if nil != chownErr {
        return "", "", chownErr
    }

    return joined, unownedDirectory, nil
}

type logDirectoryOwnership struct {
    rendersUserColumn bool
    unowned           []unownedLogDirectory
}

type unownedLogDirectory struct {
    command   string
    directory string
    user      string
}

func (instance unownedLogDirectory) warningMessage() string {
    return fmt.Sprintf("cron: the log directory %s created for %q stays owned by root: its user %q is not known on this host, so under system cron the entry may not be able to create its log file there; create the directory for that user on the host that runs it", instance.directory, instance.command, instance.user)
}

func missingDirectoriesBelow(directory string, root string) []string {
    missing := make([]string, 0)

    for current := filepath.Clean(directory); current != root && true == isWithinDir(current, root); current = filepath.Dir(current) {
        if _, statErr := os.Stat(current); nil == statErr {
            break
        }

        missing = append([]string{current}, missing...)
    }

    return missing
}

/* the doors chownCreatedLogDirectories reads the process and the system through, so a pin drives it without root */
var (
    logDirectoryEffectiveUserId = os.Geteuid
    logDirectoryLookupUser      = user.Lookup
    logDirectoryChown           = os.Chown
)

/* a user the host does not know is the ordinary build-host case: a warning, the directories stay root's */
func chownCreatedLogDirectories(commandName string, directories []string, owner string) (unownedDirectory string, err error) {
    if 0 == len(directories) || "" == owner || 0 != logDirectoryEffectiveUserId() {
        return "", nil
    }

    ownerAccount, lookupErr := logDirectoryLookupUser(owner)
    if nil != lookupErr {
        return directories[0], nil
    }

    userId, userIdErr := strconv.Atoi(ownerAccount.Uid)
    groupId, groupIdErr := strconv.Atoi(ownerAccount.Gid)
    if nil != userIdErr || nil != groupIdErr {
        return "", exception.NewError(
            "cron: the log directory created for the entry cannot be handed to its user, whose ids are not numeric",
            exceptioncontract.Context{
                "command":   commandName,
                "directory": directories[0],
                "user":      owner,
            },
            errors.Join(userIdErr, groupIdErr),
        )
    }

    for _, directory := range directories {
        if chownErr := logDirectoryChown(directory, userId, groupId); nil != chownErr {
            return "", exception.NewError(
                "cron: could not hand the log directory created for the entry to its user",
                exceptioncontract.Context{
                    "command":   commandName,
                    "directory": directory,
                    "user":      owner,
                },
                chownErr,
            )
        }
    }

    return "", nil
}

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

/* applicationIdentity refuses a name spanning lines: the ownership line has to be one line to be read back */
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

/* templateOwnedBy derives only for the package's own dialects by concrete type: an embedding wrapper keeps its rendering */
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
    commandContext *clicontract.CommandContext,
    configuration configcontract.Configuration,
    flagName string,
    parameterName string,
) string {
    if value, typed := typedFlagValue(commandContext, flagName); true == typed {
        return value
    }

    return parameterValue(configuration, parameterName)
}

func resolveDefaultPath(
    commandContext *clicontract.CommandContext,
    configuration configcontract.Configuration,
    flagName string,
    parameterName string,
) string {
    if value, typed := typedFlagValue(commandContext, flagName); true == typed {
        return value
    }

    return anchorConfiguredPath(parameterValue(configuration, parameterName), configuration)
}

/* typedFlagValue: a flag typed empty is not an answer, so the parameter behind it still is */
func typedFlagValue(commandContext *clicontract.CommandContext, flagName string) (string, bool) {
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

/* anchorConfiguredPath carries locally the rule of application/bootstrap.go, which is unexported and in another module */
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

/* errorDetailsOf answers an empty object rather than null, so the field keeps its json type */
func errorDetailsOf(runErr error) map[string]any {
    details := map[string]any{}

    var provider exceptioncontract.ContextProvider
    /* a typed-nil link satisfies As and passes a plain nil comparison; Context() on the nil receiver would panic */
    if true == exception.ChainAs(runErr, &provider) && false == isNilInterface(provider) {
        for key, value := range provider.Context() {
            details[key] = value
        }
    }

    return details
}

func errorCauseOf(runErr error) *output.ErrorCause {
    causeChain := exception.BuildCauseChain(runErr, 8)
    if 0 == len(causeChain) {
        return nil
    }

    return output.NewErrorCause(causeChain[0], map[string]any{"chain": causeChain})
}
