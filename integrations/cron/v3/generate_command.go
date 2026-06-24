package cron

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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
            Usage: "name of the registered template that will render the entries; overrides the melody.cron.template parameter (default: crontab). Built-in templates: crontab, k8s",
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
    template           Template
    outputPath         string
    logsDir            string
    binary             string
    defaultUserName    string
    heartbeatPath      string
    heartbeatCommand   []string
    heartbeatRequested []string
    heartbeatEnabled   bool
    image              string
    namespace          string
    restartPolicy      string
}

func (instance *GenerateCommand) runWithConfiguration(
    commandContext *clicontract.CommandContext,
    configuration configcontract.Configuration,
) error {
    options, resolveErr := instance.resolveRunOptions(commandContext, configuration)
    if nil != resolveErr {
        return resolveErr
    }

    entries, collectErr := instance.collectScheduledEntries(options)
    if nil != collectErr {
        return collectErr
    }

    return instance.writeDestinations(commandContext, options, entries)
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
    options.template = template

    /* @info the k8s template ignores the heartbeat (it logs to stdout and models liveness with a dedicated CronJob), so neither auto-enable a heartbeat nor demand a user for it here; an explicitly requested heartbeat still flows through so writeDestinations can warn that it is dropped */
    isK8s := TemplateNameK8s == template.Name()

    outputPath := resolveDefault(commandContext, configuration, flagNameOutput, ParameterDestinationFile)
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

    logsDir := resolveDefault(commandContext, configuration, flagNameLogsDir, ParameterLogsDir)
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

    heartbeatPath := resolveDefault(commandContext, configuration, flagNameHeartbeatPath, ParameterHeartbeatPath)
    if "" == heartbeatPath && "" != logsDir && false == isK8s && true == isHeartbeatAutoEnabled(configuration) {
        heartbeatPath = filepath.Join(logsDir, "heartbeat.crontab")
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

    options.image = resolveDefault(commandContext, configuration, flagNameImage, ParameterImage)
    options.namespace = resolveDefault(commandContext, configuration, flagNameNamespace, ParameterNamespace)
    options.restartPolicy = resolveDefault(commandContext, configuration, flagNameRestartPolicy, ParameterRestartPolicy)

    if true == options.heartbeatEnabled && false == isK8s && "" == options.defaultUserName {
        return nil, exception.NewError(
            "cron: heartbeat is configured but no user is set; pass --user, register the melody.cron.user parameter, or remove the heartbeat",
            exceptioncontract.Context{
                "flag":      flagNameDefaultUser,
                "parameter": ParameterUser,
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

    /* @info only the crontab template redirects each entry's output to a log file; the k8s template logs to container stdout and never reads Entry.LogPath, so it must not inherit the crontab-only logs-dir requirement */
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

func (instance *GenerateCommand) writeDestinations(
    commandContext *clicontract.CommandContext,
    options *runOptions,
    entries []Entry,
) error {
    writer := commandContext.Writer

    if true == options.heartbeatEnabled && TemplateNameK8s == options.template.Name() {
        _, _ = fmt.Fprintln(writer, "cron: heartbeat options are set but the k8s template ignores them; no liveness CronJob will be generated — model cluster liveness with a dedicated scheduled command instead")
    }

    /* @info the k8s namespace is a single global option, so resource-name collisions span every destination file; catch them across the whole set before any manifest is rendered or written */
    if TemplateNameK8s == options.template.Name() {
        if uniqueErr := ensureK8sNamesUnique(entries); nil != uniqueErr {
            return uniqueErr
        }
    }

    entriesByDestination, groupErr := groupEntriesByDestination(entries, options.outputPath)
    if nil != groupErr {
        return groupErr
    }

    if 0 == len(entriesByDestination) {
        if false == options.heartbeatEnabled {
            _, _ = fmt.Fprintln(writer, "the cron Configuration is empty and no --heartbeat-path or --heartbeat-command was provided; nothing to write")
            return nil
        }

        /* @info heartbeat is crontab-only; the k8s template never synthesizes a heartbeat CronJob, so an empty Configuration leaves nothing to render */
        if TemplateNameK8s == options.template.Name() {
            _, _ = fmt.Fprintln(writer, "the cron Configuration is empty and the k8s template does not emit heartbeat CronJobs; nothing to write")
            return nil
        }

        entriesByDestination[options.outputPath] = nil
    }

    destinationPaths := make([]string, 0, len(entriesByDestination))
    for destination := range entriesByDestination {
        destinationPaths = append(destinationPaths, destination)
    }
    sort.Strings(destinationPaths)

    /* @info the k8s template ignores heartbeat options entirely (see the warning above and the crontab-only render); resolving a --heartbeat-destination against the written destinations would hard-fail the command on a setting it just declared ignored, so the requested destinations are dropped for k8s */
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
        return heartbeatDestinationsErr
    }

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
            return exception.NewError(
                fmt.Sprintf("cron: could not render %s content for %s: %s", options.template.Name(), destination, renderErr.Error()),
                exceptioncontract.Context{
                    "template":    options.template.Name(),
                    "destination": destination,
                },
                renderErr,
            )
        }

        if mkdirErr := os.MkdirAll(filepath.Dir(destination), 0o755); nil != mkdirErr {
            return exception.NewError(
                "cron: could not create the output directory",
                exceptioncontract.Context{"directory": filepath.Dir(destination)},
                mkdirErr,
            )
        }

        if writeErr := atomicWriteFile(destination, []byte(content), 0o644); nil != writeErr {
            return writeErr
        }

        if 0 == len(destinationEntries) && true == options.heartbeatEnabled {
            _, _ = fmt.Fprintf(writer, "wrote heartbeat-only file to %s\n", destination)
        } else {
            _, _ = fmt.Fprintf(writer, "wrote %d entries to %s\n", len(destinationEntries), destination)
        }
    }

    return nil
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

        entry := Entry{
            Name:            commandName,
            User:            user,
            Schedule:        config.Schedule,
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

    /* @info the active template does not redirect output to a log file (k8s), so there is no log path to resolve and no logs-dir to demand */
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

    return joined, nil
}

func configurationFromRuntime(runtimeInstance runtimecontract.Runtime) (configcontract.Configuration, error) {
    configuration, fromContainerErr := container.FromResolver[configcontract.Configuration](runtimeInstance.Container(), melodyconfig.ServiceConfig)
    if nil != fromContainerErr {
        return nil, exception.NewError(
            "cron: could not resolve the configuration service from the container",
            exceptioncontract.Context{"service": melodyconfig.ServiceConfig},
            fromContainerErr,
        )
    }

    return configuration, nil
}

func resolveDefault(
    commandContext *clicontract.CommandContext,
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

func isHeartbeatAutoEnabled(configuration configcontract.Configuration) bool {
    if nil == configuration {
        return false
    }

    parameter := configuration.Get(ParameterHeartbeatAutoEnabled)
    if nil == parameter {
        return false
    }

    enabled, parameterBoolErr := parameter.Bool()
    if nil != parameterBoolErr {
        return false
    }

    return enabled
}

var _ clicontract.Command = (*GenerateCommand)(nil)
