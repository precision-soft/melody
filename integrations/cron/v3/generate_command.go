package cron

import (
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

/* RegisterTemplate installs the template under its name, replacing an existing one silently: replacement by name is the door through which an application overrides a builtin dialect. Two application templates sharing a name resolve to the later registration, so a caller that must not replace anything checks the name first. */
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

/* ownFlags keeps the generator's flags apart from the standard set every melody command carries: the framework rewrites -v/-vv into --verbosity for every command, so a command without the standard flags dies on the framework's own convention with "flag provided but not defined". */
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
            Usage: "empty the destinations in dir(--out) that this generator wrote earlier and this run no longer produces, so an entry retired or moved between versions stops running. Only files carrying the current template's ownership marker are touched, and destinations outside dir(--out) are never swept",
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
    image              string
    namespace          string
    restartPolicy      string
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

    /* the report is a defer so that no failure path can leave the run without a document: under --format=json every early return used to travel straight out past the one door that builds the envelope, and the cli silences the command's own error line in json mode, so `app melody:cron:generate --format=json | jq …` received an empty stream — indistinguishable from a missing binary — for a malformed schedule or an unwritable directory. The sibling integration's commands have carried this shape since the verdict that gave migrate its machine contract. */
    defer func() {
        runErr = instance.reportWrites(commandContext, option, startedAt, writes, pruned, emptyMessage, warnings, runErr)
    }()

    options, resolveErr := instance.resolveRunOptions(commandContext, configuration)
    if nil != resolveErr {
        return resolveErr
    }

    /* the k8s template ignores the heartbeat entirely (it logs to stdout and models liveness with a dedicated CronJob), so an explicitly requested one is reported as dropped rather than silently swallowed or hard-failed on a setting the template just declared ignored. */
    if true == options.heartbeatEnabled && TemplateNameK8s == options.template.Name() {
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
    writes, pruned, emptyMessage, writeErr = instance.writeDestinations(option, options, entries)

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
    options.template = template

    /* the builtin k8s template ignores the heartbeat (it logs to stdout and models liveness with a dedicated CronJob), so a heartbeat path is never auto-derived for it; an explicitly requested heartbeat still flows through so the run can warn that it is dropped */
    isK8s := TemplateNameK8s == template.Name()

    /* a dialect that renders no user column at all (busybox crond, per-user crontabs, a CronJob manifest) never needs a user to place the heartbeat line — demanding one turns a valid configuration into a hard error */
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

    options.binary = resolveDefault(commandContext, configuration, flagNameBinary, ParameterBinary)
    options.defaultUserName = resolveDefault(commandContext, configuration, flagNameDefaultUser, ParameterUser)

    heartbeatPath := resolveDefaultPath(commandContext, configuration, flagNameHeartbeatPath, ParameterHeartbeatPath)
    if "" == heartbeatPath && "" != logsDir {
        autoEnabled, autoEnabledErr := isHeartbeatAutoEnabled(configuration)
        if nil != autoEnabledErr {
            return nil, autoEnabledErr
        }

        /* the malformed opt-in fails under every template — a typo is a typo whichever dialect renders — while the derived path stays crontab-only, because the k8s template ignores the heartbeat and deriving one for it would only arm the dropped-heartbeat warning on a setting nobody made */
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

    /* only the crontab template redirects each entry's output to a log file; the k8s template logs to container stdout and never reads Entry.LogPath, so it must not inherit the crontab-only logs-dir requirement */
    requiresLogPath := TemplateNameK8s != options.template.Name()

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

/* writeDestinations answers what it wrote, what there was to say about an empty run and what stopped it, leaving the one report door to the caller's defer: reporting from here left the seven failure paths below with no document at all, and the writes accumulated before a failure are exactly what tells the consumer which files a partial run left on disk — the destinations are written one by one with no rollback. */
func (instance *GenerateCommand) writeDestinations(
    option output.Option,
    options *runOptions,
    entries []Entry,
) ([]destinationWrite, []string, string, error) {
    /* the k8s namespace is a single global option, so resource-name collisions span every destination file; catch them across the whole set before any manifest is rendered or written */
    if TemplateNameK8s == options.template.Name() {
        if uniqueErr := ensureK8sNamesUnique(entries); nil != uniqueErr {
            return nil, nil, "", uniqueErr
        }
    }

    entriesByDestination, groupErr := groupEntriesByDestination(entries, options.outputPath)
    if nil != groupErr {
        return nil, nil, "", groupErr
    }

    /* an empty configuration still sweeps: emptying the configuration is exactly the version in which every destination the previous one wrote is stale, and answering "nothing to write" while leaving them all live is the worst of the three cases. It stays a success either way — a run that has nothing to write has not failed. */
    if 0 == len(entriesByDestination) && false == options.heartbeatEnabled {
        pruned, pruneErr := pruneStaleDestinations(options, nil)

        return nil, pruned, "the cron Configuration is empty and no --heartbeat-path or --heartbeat-command was provided; nothing to write", pruneErr
    }

    if 0 == len(entriesByDestination) && true == options.heartbeatEnabled {
        /* heartbeat is crontab-only; the k8s template never synthesizes a heartbeat CronJob, so an empty Configuration leaves nothing to render — but the sweep still runs, for the same emptied-configuration reason as above */
        if TemplateNameK8s == options.template.Name() {
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

    /* the k8s template ignores heartbeat options entirely (see the dropped-heartbeat warning and the crontab-only render); resolving a --heartbeat-destination against the written destinations would hard-fail the command on a setting it just declared ignored, so the requested destinations are dropped for k8s */
    heartbeatRequested := options.heartbeatRequested
    if TemplateNameK8s == options.template.Name() {
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

/* pruneStaleDestinations empties the destinations this generator wrote earlier and this run no longer produces. Without it a version that retires an entry leaves its file untouched and crond keeps running the retired job forever, and a version that MOVES an entry to another destination leaves it live in both — the double execution the runner refuses at construction, produced silently by the generator.

Three rules bound what it may touch, because emptying a file is not reversible. It is opt-in, so a deployment that manages the directory itself is unaffected. It reads only the output directory, never recursing and never following a destination an entry placed elsewhere by absolute path — those live where the operator put them and are not this directory's to reconcile. And it empties only a file whose leading lines carry the ownership marker of the template generating now as an exact line of their own, so a file this generator cannot prove it wrote — one that merely quotes the marker included — is left exactly as it is.

Emptying means rendering the template with no entries: the destination keeps its header and with it the marker, so it stays recognizable to the next run rather than becoming an unowned file the sweep would refuse to touch ever again. */
func pruneStaleDestinations(options *runOptions, writes []destinationWrite) ([]string, error) {
    if false == options.prune {
        return nil, nil
    }

    ownedTemplate, isOwnedTemplate := options.template.(OwnedTemplate)
    if false == isOwnedTemplate {
        return nil, nil
    }

    marker := ownedTemplate.OwnershipMarker()
    if "" == strings.TrimSpace(marker) {
        return nil, nil
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
        /* only a regular file is a candidate. Skipping directories alone left the sweep opening whatever else the directory held, and opening a fifo with no writer blocks forever: one named pipe beside the destinations wedged the generator inside os.Open with no deadline and no diagnostic. A device, a socket and a symlink are refused for the same reason — this generator wrote none of them, so none of them can be one of its own. */
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

/* ownershipMarkerLineLimit bounds WHERE in that head the marker may stand: every builtin template renders it inside the leading comment block, within the first few lines. The old check was a substring search over the whole 8KiB head, and emptying is irreversible — an operator's README, playbook or commented backup that merely QUOTED the marker anywhere in its opening kilobytes was emptied as if this generator had written it. */
const ownershipMarkerLineLimit = 10

/* fileCarriesOwnershipMarker recognises ownership by an EXACT marker line among the file's leading lines. Exactness is what keeps two markers apart when one extends the other: a custom dialect that declares its own marker by suffixing the builtin one (the ansible example does) documents its files as its own, and a substring match read the builtin marker inside the longer line and emptied the custom dialect's files from a builtin run. The builtin dialects share one identical marker line, so the deliberate cross-dialect reconciliation between them is untouched. */
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

    lines := strings.SplitN(string(head[:read]), "\n", ownershipMarkerLineLimit+1)
    if ownershipMarkerLineLimit < len(lines) {
        lines = lines[:ownershipMarkerLineLimit]
    }

    for _, line := range lines {
        if marker == strings.TrimSpace(line) {
            return true, nil
        }
    }

    return false, nil
}

/* reportWrites is the generator's one report door, reached from the run's defer on every path: the written summary as text lines, or as the single machine-readable document under --format=json — the failure inside it, beside whatever the run had already written before it stopped. The summary is essential output — the command's whole visible result — so --quiet, which suppresses headers and non-essential output, does not silence it.

The run's own failure stays the verdict the command returns; a rendering failure becomes one only when the run itself succeeded, which is the rule the sibling integration's exit door states in the same words. In text mode the failure travels alone, as it always has: the cli entry point prints it. */
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

        /* pruned is a list on every run, empty rather than null when nothing was swept or the sweep was never asked for: a field whose json type changes with the outcome cannot be consumed at all, which is the rule the machine contracts of the debug family were put on */
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
            /* the details and the cause used to be nil on every failure alike, so the machine document — the one a deploy pipeline reads — was the single rendering that threw away what the error already carried: a failed rename filed the destination and the source in the journal at the same instant and answered `"details":null,"cause":null` on stdout. The details object stays an object when the failure carries no context, so the field keeps its json type on every failure. */
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

    /* a run that fails part way through still names what it already did, the way the json branch beside it does. Emptying a destination is irreversible and the sweep returns the destinations it emptied beside its failure, so returning here without printing them left the operator of a failed deploy with no record at all of which manifests had just been blanked — not one "pruned" line, not even the "wrote" lines of the writes that had succeeded. */
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

    if chmodErr := os.Chmod(tmpPath, mode); nil != chmodErr {
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

        /* every entry carries its own schedule: Render is userland, Schedule.Defaults is documented as mutating in place, and a template that calls it on the schedule it was handed would otherwise rewrite the one behind every sibling entry of this command — and, before Entries copied, the registered one for the rest of the process */
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

            /* the entry's own arguments come last, after the instance flags this generator adds, so the manifest line runs the command line the entry declared — the same one the in-process runner hands the child. A Configuration drives both halves, and an argument honoured by only one of them is a divergence nothing would report. */
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

    /* a LogFileName carrying a subdirectory ("nightly/report.log") stays within the logs dir and passes the guard above, but nothing else ever creates that subdirectory — and under system cron the shell aborts the whole command when the >> redirection cannot create its file, so the scheduled job silently never runs. The destination side already creates its parent the same way. */
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

/* configurationFromRuntime resolves through the run's scope with the container as the fallback, the way every other command on this seam reads its services: a scope-level substitution of the configuration is honoured here exactly as the framework's own runtime door honours it. */
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

func resolveDefault(
    commandContext clicontract.Context,
    configuration configcontract.Configuration,
    flagName string,
    parameterName string,
) string {
    if true == commandContext.IsSet(flagName) {
        value := commandContext.String(flagName)
        if "" != value {
            return value
        }
    }

    if nil != configuration {
        parameter := configuration.Get(parameterName)
        if nil != parameter {
            return parameter.String()
        }
    }

    return ""
}

/* resolveDefaultPath is resolveDefault for a value that names a path, and it differs in one thing: a relative path that came from a PARAMETER is anchored at the project directory, while one typed as a cli FLAG stays relative to the working directory. The two sources answer to different conventions. A flag is typed in a shell, next to the paths that shell already resolves, and anchoring it elsewhere would surprise every operator. A parameter is part of the application's configuration and belongs to the project: melody resolves MELODY_LOG_PATH, kernel.logs_dir and kernel.cache_dir against the project directory for exactly that reason, and cron was the one place where "melody.cron.logs_dir = var/log/cron" meant a different directory depending on where the binary was invoked from — under a supervisor that starts from /, the generated crontab baked /var/log/cron into itself. The shipped defaults hid it by carrying %kernel.project_dir% themselves. */
func resolveDefaultPath(
    commandContext clicontract.Context,
    configuration configcontract.Configuration,
    flagName string,
    parameterName string,
) string {
    if true == commandContext.IsSet(flagName) {
        value := commandContext.String(flagName)
        if "" != value {
            return value
        }
    }

    if nil == configuration {
        return ""
    }

    parameter := configuration.Get(parameterName)
    if nil == parameter {
        return ""
    }

    return anchorConfiguredPath(parameter.String(), configuration)
}

/* anchorConfiguredPath carries locally the rule application/bootstrap.go applies to every other melody runtime path; it cannot call that door, which is unexported and in another module. A configuration that names no project directory keeps the working-directory anchoring, because there is nothing better to anchor to and refusing would turn a generator that works today into a boot failure. */
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

/* isHeartbeatAutoEnabled separates an unset opt-in from a malformed one. Reading a value the parameter cannot convert as "not enabled" would generate a crontab without the liveness line the operator asked for and report success — the misspelling would be indistinguishable from never having asked. */
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

/* errorDetailsOf renders the failure's own context as the json envelope's details, an empty object rather than null when it carries none: a field whose json type changes with the outcome cannot be consumed at all. It is written here rather than shared with the migrate integration because the two are separate modules. */
func errorDetailsOf(runErr error) map[string]any {
    details := map[string]any{}

    var provider exceptioncontract.ContextProvider
    if true == errors.As(runErr, &provider) && nil != provider {
        for key, value := range provider.Context() {
            details[key] = value
        }
    }

    return details
}

/* errorCauseOf answers the failure's text together with the whole chain beneath it. The chain starts at the failure itself rather than one link below, because this envelope's message is a fixed label — "the cron manifest generation failed" — so the cause is where the failure's own sentence lives; the migrate integration's envelope puts that sentence in the message and its cause therefore starts one link lower. */
func errorCauseOf(runErr error) *output.ErrorCause {
    causeChain := exception.BuildCauseChain(runErr, 8)
    if 0 == len(causeChain) {
        return nil
    }

    return output.NewErrorCause(causeChain[0], map[string]any{"chain": causeChain})
}
