package application

import (
    "fmt"
    "io"
    "os"
    "reflect"
    "sort"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/debug"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v3/http/middleware/pipeline"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/messagebus"
    "github.com/precision-soft/melody/v3/openapi"
    "github.com/precision-soft/melody/v3/runtime"
    "github.com/precision-soft/melody/v3/security"
)

type commandSuggestion struct {
    Name        string
    Description string
}

func (instance *Application) RegisterCliCommand(command clicontract.Command) {
    if true == instance.booted {
        exception.Panic(
            exception.NewError(
                "cannot register cli commands after boot",
                nil,
                nil,
            ),
        )
    }

    /* read through the interface: a typed nil passes a plain comparison and reaches command.Name() below */
    if true == internal.IsNilInterface(command) {
        exception.Panic(
            exception.NewError(
                "cli command may not be nil",
                nil,
                nil,
            ),
        )
    }

    /* the name is judged trimmed, the spelling the cli registration dispatches under, so two names differing only in padding cannot both pass */
    commandName := strings.TrimSpace(command.Name())
    if "" == commandName {
        exception.Panic(
            exception.NewError(
                "cli command name may not be empty",
                exceptioncontract.Context{
                    "commandType": reflect.TypeOf(command).String(),
                },
                nil,
            ),
        )
    }

    for _, existingCommand := range instance.cliCommands {
        if commandName == strings.TrimSpace(existingCommand.Name()) {
            /* recorded for the aggregated boot report; the first registration wins until the report ends the boot */
            instance.recordBootCollision(bootCollisionKindCliCommand, commandName)
            return
        }
    }

    instance.cliCommands = append(instance.cliCommands, command)
}

func (instance *Application) bootCli() {
    debugCommands := []clicontract.Command{
        &debug.RouterCommand{},
    }

    if config.EnvDevelopment == instance.configuration.Kernel().Env() {
        debugCommands = append(
            debugCommands,
            &debug.ContainerCommand{},
            &debug.ParameterCommand{},
            debug.NewEventCommand(instance.securityDeferredListeners),
            debug.NewMiddlewareCommand(
                func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
                    return instance.httpMiddlewares.describe(instance.kernel)
                },
                func() ([]httpcontract.Middleware, error) {
                    return instance.httpMiddlewares.buildForInspection(instance.kernel)
                },
            ),
            /* the application slot stays empty: the application's own version arrives through output.SetApplicationVersion, and one never declared reads <unknown> */
            &debug.VersionCommand{},
        )
    }

    for _, commandInstance := range debugCommands {
        instance.RegisterCliCommand(commandInstance)
    }

    instance.registerCoreCliCommands()
}

/* registerCoreCliCommands contributes the framework-owned core commands: the route manifest always, the openapi and messagebus consume commands only when the backing service is registered, the latter only with a resolvable bus. A command the application already registered under the same name is skipped. */
func (instance *Application) registerCoreCliCommands() {
    serviceContainer := instance.kernel.ServiceContainer()

    instance.registerCoreCliCommandIfAbsent(http.NewRouteManifestCommand())

    if true == serviceContainer.Has(openapi.ServiceOpenApiRegistry) {
        instance.registerCoreCliCommandIfAbsent(openapi.NewGenerateCommandFromContainer())
    }

    hasBus := true == serviceContainer.Has(messagebus.ServiceConsumeBus) || true == serviceContainer.Has(messagebus.ServiceBus)
    if true == serviceContainer.Has(messagebus.ServiceTransports) && true == hasBus {
        instance.registerCoreCliCommandIfAbsent(messagebus.NewConsumeCommandFromContainer())
    }
}

func (instance *Application) registerCoreCliCommandIfAbsent(command clicontract.Command) {
    for _, existingCommand := range instance.cliCommands {
        if existingCommand.Name() == command.Name() {
            return
        }
    }

    instance.RegisterCliCommand(command)
}

/* securityDeferredListeners feeds debug:events with the security pair only the serving process wires, so a console dispatcher names it instead of rendering an absence. A process without a compiled security configuration declares nothing, and neither does the serving process, where the pair is registered for real. */
func (instance *Application) securityDeferredListeners() []debug.DeferredListener {
    if nil == instance.securityConfiguration {
        return nil
    }

    if config.ModeHttp == instance.runtimeFlags.Mode() {
        return nil
    }

    deferredNote := "registered only in the http serving process"

    return []debug.DeferredListener{
        {
            EventName:    kernelcontract.EventKernelRequest,
            Priority:     security.KernelFirewallListenerPriority,
            ListenerName: "security resolution listener",
            Note:         deferredNote,
        },
        {
            EventName:    kernelcontract.EventKernelRequest,
            Priority:     security.KernelAccessControlListenerPriority,
            ListenerName: "security access control listener",
            Note:         deferredNote,
        },
    }
}

func (instance *Application) runCli() error {
    kernelInstance := instance.kernel
    configuration := instance.configuration

    serviceContainer := kernelInstance.ServiceContainer()

    logger := logging.LoggerMustFromContainer(serviceContainer)

    scope := serviceContainer.NewScope()

    /* a last-resort net for a panic between the scope's creation and the action dispatch; a run that reaches the action has its scope closed there, so this second close answers nil */
    defer func() {
        scopeCloseErr := scope.Close()
        if nil != scopeCloseErr {
            logging.EmergencyLogger().Error("failed to close service container scope", exception.LogContext(scopeCloseErr))
        }
    }()

    runtimeInstance := runtime.New(instance.ctx, scope, serviceContainer)

    processId := logging.GenerateProcessId()
    loggerWithProcess := logging.NewProcessLogger(logger, processId, "processId")

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, loggerWithProcess)

    /* the console counterpart of the request context the http kernel installs, so a scoped service can resolve the run's identity; the instant comes from the kernel's clock */
    scope.MustOverrideProtectedInstance(ServiceProcessContext, NewProcessContext(processId, kernelInstance.Clock().Now()))

    loggerWithProcess.Info("starting cli application", nil)

    rootCli := cli.NewRoot(configuration.Cli().Name(), configuration.Cli().Description())
    rootCli.SetWriter(os.Stdout)
    rootCli.SetErrorWriter(os.Stderr)

    availableCommands := make([]commandSuggestion, 0, len(instance.cliCommands))

    for _, command := range instance.cliCommands {
        availableCommands = append(
            availableCommands,
            commandSuggestion{
                /* trimmed to the dispatched spelling, which the suggestion gate compares against */
                Name:        strings.TrimSpace(command.Name()),
                Description: command.Description(),
            },
        )

        cli.Register(rootCli, command, runtimeInstance)
    }

    normalizedArguments := normalizeCliVerbosityArguments(os.Args)

    suggestCliCommandErr := suggestCliCommand(normalizedArguments, availableCommands, kernelInstance.Clock())
    if nil != suggestCliCommandErr {
        return suggestCliCommandErr
    }

    return rootCli.Run(instance.ctx, normalizedArguments)
}

func normalizeCliVerbosityArguments(arguments []string) []string {
    if 0 == len(arguments) {
        return arguments
    }

    normalized := make([]string, 0, len(arguments))
    stopNormalization := false

    for _, argument := range arguments {
        if true == stopNormalization {
            normalized = append(normalized, argument)
            continue
        }

        if "--" == argument {
            stopNormalization = true
            normalized = append(normalized, argument)
            continue
        }

        if true == strings.HasPrefix(argument, "-") && false == strings.HasPrefix(argument, "--") {
            isVerbosityShortFlag := true

            if 2 > len(argument) {
                isVerbosityShortFlag = false
            }

            if true == isVerbosityShortFlag && false == strings.HasPrefix(argument, "-v") {
                isVerbosityShortFlag = false
            }

            if true == isVerbosityShortFlag {
                for _, runeValue := range argument[2:] {
                    if 'v' != runeValue {
                        isVerbosityShortFlag = false
                        break
                    }
                }
            }

            if true == isVerbosityShortFlag {
                verbosityLevel := len(argument) - 1
                normalized = append(
                    normalized,
                    fmt.Sprintf(
                        "--%s=%d",
                        output.FlagNameVerbosity,
                        verbosityLevel,
                    ),
                )

                continue
            }
        }

        normalized = append(normalized, argument)
    }

    return normalized
}

func suggestCliCommand(
    arguments []string,
    availableCommands []commandSuggestion,
    clockInstance clockcontract.Clock,
) error {
    if 2 > len(arguments) {
        return nil
    }

    commandName := strings.TrimSpace(arguments[1])
    if "" == commandName {
        return nil
    }

    if true == strings.HasPrefix(commandName, "-") {
        return nil
    }

    lowercaseCommandName := strings.ToLower(commandName)

    if "help" == lowercaseCommandName || "h" == lowercaseCommandName {
        return nil
    }

    for _, availableCommand := range availableCommands {
        if availableCommand.Name == commandName {
            return nil
        }
    }

    startedAt := clockInstance.Now()

    option := output.NormalizeOption(
        output.Option{
            Format:  output.FormatTable,
            NoColor: true,
            Verbose: false,
            Quiet:   true,
            Order:   output.SortOrderAscending,
            Limit:   0,
            Offset:  0,
        },
    )

    meta := output.NewMeta(
        "cli:suggest",
        []string{commandName},
        option,
        startedAt,
        time.Duration(0),
        output.Version{},
    )

    envelope := output.NewEnvelope(meta)

    matchingCommands := make([]commandSuggestion, 0, len(availableCommands))

    for _, availableCommand := range availableCommands {
        if strings.Contains(strings.ToLower(availableCommand.Name), lowercaseCommandName) {
            matchingCommands = append(matchingCommands, availableCommand)
        }
    }

    sort.Slice(matchingCommands, func(leftIndex int, rightIndex int) bool {
        return matchingCommands[leftIndex].Name < matchingCommands[rightIndex].Name
    })

    builder := output.NewTableBuilder()

    const maxMatchesToPrint = 50

    if 0 == len(matchingCommands) {
        envelope.SetError(
            "cli.commandNotFound",
            "cli command not found",
            map[string]any{
                "command": commandName,
            },
            nil,
        )

        builder.AddSummaryLine("MATCHES: 0")

        block := builder.AddBlock(
            "AVAILABLE COMMANDS",
            []string{"command", "description"},
        )

        for _, availableCommand := range availableCommands {
            description := strings.TrimSpace(availableCommand.Description)
            if "" == description {
                description = "-"
            }

            block.AddRow(
                availableCommand.Name,
                description,
            )
        }

        envelope.Table = builder.Build()

        envelope.Meta.DurationMilliseconds = clockInstance.Now().Sub(startedAt).Milliseconds()

        printCliCommandNotFoundHeader(os.Stderr, commandName, startedAt)

        _ = output.Render(os.Stderr, envelope, option)

        /* returned unmarked so the exit path writes it to the application log; the rendered table lives only on stderr */
        commandNotFoundErr := exception.NewError(
            "cli command not found",
            exceptioncontract.Context{
                "command": commandName,
            },
            nil,
        )

        return exception.NewExitError(2, commandNotFoundErr)
    }

    matchesToPrint := matchingCommands
    if maxMatchesToPrint < len(matchingCommands) {
        matchesToPrint = matchingCommands[:maxMatchesToPrint]
    }

    envelope.SetError(
        "cli.commandNotFound",
        "cli command not found, matches found",
        map[string]any{
            "command":        commandName,
            "matchesTotal":   len(matchingCommands),
            "matchesPrinted": len(matchesToPrint),
        },
        nil,
    )

    builder.AddSummaryLine(
        fmt.Sprintf(
            "MATCHES: %d total | %d shown",
            len(matchingCommands),
            len(matchesToPrint),
        ),
    )

    block := builder.AddBlock(
        "SUGGESTED COMMANDS",
        []string{"command", "description"},
    )

    for _, match := range matchesToPrint {
        description := strings.TrimSpace(match.Description)
        if "" == description {
            description = "-"
        }

        block.AddRow(
            match.Name,
            description,
        )
    }

    if maxMatchesToPrint < len(matchingCommands) {
        builder.AddSummaryLine(
            fmt.Sprintf(
                "... and %d more",
                len(matchingCommands)-maxMatchesToPrint,
            ),
        )
    }

    envelope.Table = builder.Build()

    envelope.Meta.DurationMilliseconds = clockInstance.Now().Sub(startedAt).Milliseconds()

    printCliCommandNotFoundHeader(os.Stderr, commandName, startedAt)

    _ = output.Render(os.Stderr, envelope, option)

    /* returned unmarked so the exit path writes it to the application log; the rendered table lives only on stderr */
    matchesFoundErr := exception.NewError(
        "cli command not found, matches found",
        exceptioncontract.Context{
            "command":        commandName,
            "matchesTotal":   len(matchingCommands),
            "matchesPrinted": len(matchesToPrint),
        },
        nil,
    )

    return exception.NewExitError(2, matchesFoundErr)
}

/* printCliCommandNotFoundHeader writes plain text, since the suggestion table below it is rendered under NoColor. The command name comes from argv and is escaped as in every sibling channel, so an embedded carriage return or escape sequence cannot repaint the line. */
func printCliCommandNotFoundHeader(writer io.Writer, commandName string, startedAt time.Time) {
    const logFiller = "======================================"

    _, _ = fmt.Fprintf(
        writer,
        "%s [command not found] [%s] [%s] %s\n",
        logFiller,
        internal.EscapeControlCharacters(commandName),
        startedAt.Format(time.DateTime),
        logFiller,
    )
}
