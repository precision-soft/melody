package application

import (
    "context"
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

    /* read through the interface: a typed nil passes a plain comparison and reaches command.Name() three lines below */
    if true == internal.IsNilInterface(command) {
        exception.Panic(
            exception.NewError(
                "cli command may not be nil",
                nil,
                nil,
            ),
        )
    }

    /* the name is judged trimmed because that is the name the command is dispatched under: the cli registration trims before registering, so a padded name accepted raw here would pass this gate and then either collide at every dispatch — two names differing only in padding — or register under a spelling no argv can produce, with the suggestion table blocking every invocation of a command that exists. */
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
            /* recorded for the aggregated boot report instead of panicking one at a time; the first registration wins until the guaranteed panic ends the boot */
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
            /* the application slot stays empty on purpose: the application's own version arrives through output.SetApplicationVersion, and melody's version filled in here made debug:version print the framework version twice — an application that never declared its version reads <unknown> instead of a lie */
            &debug.VersionCommand{},
        )
    }

    for _, commandInstance := range debugCommands {
        instance.RegisterCliCommand(commandInstance)
    }

    instance.registerCoreCliCommands()
}

/* registerCoreCliCommands contributes the framework-owned core commands the same way the debug commands are contributed: the application does not have to wire them by hand. The route manifest is always available (it only needs the router, which the framework always registers); the openapi and messagebus consume commands are exposed only when their feature is configured — detected by the presence of the backing service — so an application that does not use them sees no dead command. The messagebus command additionally requires a resolvable bus so it never surfaces a command that would panic at run time. Each command is skipped when the application already registered one of the same name, so an app still wiring a core command by hand keeps working instead of panicking on a duplicate. */
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

/* securityDeferredListeners feeds the declaration channel of debug:events with what only the serving process wires: the security pair stays http-only by design, so a console dispatcher can never show it and the command says so instead of rendering an absence. A process without a compiled security configuration declares nothing, and the serving process itself declares nothing either — there the pair is registered for real and the dispatcher answers. */
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

    /* a last-resort net for a panic between the scope's creation and the action dispatch: on every run that reaches the action, the action itself closes the scope and reports a teardown failure beside the command's own error — through the run's returned error, into the exit owner's record — so this second close answers nil and the branch below stays silent. */
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

    /* the console counterpart of the request context the http kernel installs into each request's scope: the run's identity lives where a scoped service can resolve it, instead of being computed for the logger and thrown away. The instant comes from the kernel's clock, so a test that froze the clock reads the same start instant everywhere the process context travels. */
    scope.MustOverrideProtectedInstance(ServiceProcessContext, NewProcessContext(processId, kernelInstance.Clock().Now()))

    loggerWithProcess.Info("starting cli application", nil)

    rootCli := cli.NewCommandContext(configuration.Cli().Name(), configuration.Cli().Description())
    rootCli.Writer = os.Stdout
    rootCli.ErrWriter = os.Stderr
    rootCli.ExitErrHandler = func(handlerContext context.Context, handlerCommandContext *clicontract.CommandContext, handlerErr error) {
    }

    availableCommands := make([]commandSuggestion, 0, len(instance.cliCommands))

    for _, command := range instance.cliCommands {
        availableCommands = append(
            availableCommands,
            commandSuggestion{
                /* trimmed to the dispatched spelling: the suggestion gate compares the trimmed input against this list, and a raw padded name here would fail the exact match and block a command that exists */
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

        /* returned unmarked so the exit path writes it to the application log: the rendered table lives only on stderr, and a run refused here used to be invisible to anything reading the log file */
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

    /* returned unmarked so the exit path writes it to the application log: the rendered table lives only on stderr, and a run refused here used to be invisible to anything reading the log file */
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

/* printCliCommandNotFoundHeader writes plain text: the suggestion table below it is rendered under NoColor, and a header carrying ansi sequences around a colorless table would contradict the very option it was rendered with */
func printCliCommandNotFoundHeader(writer io.Writer, commandName string, startedAt time.Time) {
    const logFiller = "======================================"

    _, _ = fmt.Fprintf(
        writer,
        "%s [command not found] [%s] [%s] %s\n",
        logFiller,
        commandName,
        startedAt.Format(time.DateTime),
        logFiller,
    )
}
