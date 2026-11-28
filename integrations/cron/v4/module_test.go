package cron

import (
    "errors"
    "slices"
    "strings"
    "testing"
    "time"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type spyEnvironmentKeyRegistrar struct {
    keys     []string
    defaults map[string]string
}

func (instance *spyEnvironmentKeyRegistrar) DeclareEnvironmentKey(key configcontract.EnvironmentKey) {
    instance.keys = append(instance.keys, key.Key)

    if nil == instance.defaults {
        instance.defaults = make(map[string]string)
    }
    instance.defaults[key.Key] = key.Default
}

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "cron" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "cron")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_DeclaresItsKeysWithoutAnOptIn(t *testing.T) {
    registrar := &spyEnvironmentKeyRegistrar{}
    NewModule(ModuleConfig{}).RegisterEnvironmentKeys(registrar)

    if 10 != len(registrar.keys) || KeyUser != registrar.keys[0] || KeyRestartPolicy != registrar.keys[9] {
        t.Fatalf("expected the ten keys declared, got %v", registrar.keys)
    }

    if _, isParameterModule := any(NewModule(ModuleConfig{})).(applicationcontract.ParameterModule); true == isParameterModule {
        t.Fatalf("expected the module to register no parameter by hand")
    }
}

func TestModule_DeclaresItsKeysWithTheConfiguredDefaults(t *testing.T) {
    registrar := &spyEnvironmentKeyRegistrar{}
    NewModule(ModuleConfig{KeyDefaults: KeyDefaults{User: "www-data", RestartPolicy: "Never"}}).RegisterEnvironmentKeys(registrar)

    if "www-data" != registrar.defaults[KeyUser] || "Never" != registrar.defaults[KeyRestartPolicy] {
        t.Fatalf("expected ModuleConfig.KeyDefaults as the declared defaults, got %v", registrar.defaults)
    }
}

func TestModule_RegisterCliCommandsReturnsNilWithoutConfiguration(t *testing.T) {
    if commands := NewModule(ModuleConfig{}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands without a configuration, got %d", len(commands))
    }
}

func TestModule_RegisterCliCommandsFromConfiguration(t *testing.T) {
    commands := NewModule(ModuleConfig{Configuration: staticConfiguration(NewConfiguration())}).RegisterCliCommands(nil)
    if 1 != len(commands) || "melody:cron:generate" != commands[0].Name() {
        t.Fatalf("expected the melody:cron:generate command, got %v", commands)
    }
}

/* the schedule steps the day of month across the odd days and pins Monday; 2026-07-20 is an even-numbered Monday, matched only by the kubernetes dialect's or rule, so the assertions prove ModuleConfig.RunnerDialect reaches the runner's matchers and that the zero value keeps the crontab default. */
func TestModule_RegisterCliCommandsThreadsTheRunnerDialect(t *testing.T) {
    evenMonday := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
    if time.Monday != evenMonday.Weekday() {
        t.Fatalf("expected 2026-07-20 to be a Monday, got a %s", evenMonday.Weekday())
    }

    newModuleConfig := func(dialect RunnerDialect) ModuleConfig {
        job := newRecordingCommand("job:odd-mondays")

        return ModuleConfig{
            Configuration: staticConfiguration(NewConfiguration().
                Schedule("job:odd-mondays", EntryConfig{
                    Schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "*/2", DayOfWeek: "1"},
                })),
            Runner: RunnerConfig{Dialect: dialect, Commands: []clicontract.Command{job}},
        }
    }

    runnerOf := func(t *testing.T, config ModuleConfig) *RunnerCommand {
        t.Helper()

        commands := NewModule(config).RegisterCliCommands(nil)

        for _, command := range commands {
            if runner, isRunner := command.(*RunnerCommand); true == isRunner {
                return runner
            }
        }

        t.Fatal("expected the module to register the runner command")

        return nil
    }

    zeroValueRunner := runnerOf(t, newModuleConfig(""))
    if true == zeroValueRunner.entries[0].matcher.Matches(evenMonday) {
        t.Fatal("expected the zero-value dialect to keep the crontab and rule for an even-numbered Monday")
    }

    kubernetesRunner := runnerOf(t, newModuleConfig(RunnerDialectKubernetes))
    if false == kubernetesRunner.entries[0].matcher.Matches(evenMonday) {
        t.Fatal("expected the kubernetes dialect to apply the or rule for an even-numbered Monday")
    }
}

func TestModule_RegisterCliCommandsUnknownRunnerDialectPanics(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for an unknown runner dialect")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrUnknownRunnerDialect) {
            t.Fatalf("expected ErrUnknownRunnerDialect, got %v", recoveredErr)
        }
    }()

    job := newRecordingCommand("job:top")

    module := NewModule(ModuleConfig{
        Configuration: staticConfiguration(NewConfiguration().
            Schedule("job:top", EntryConfig{Schedule: &Schedule{Minute: "0"}})),
        Runner: RunnerConfig{Dialect: RunnerDialect("solaris"), Commands: []clicontract.Command{job}},
    })

    module.RegisterCliCommands(nil)
}

func TestModule_RegisterCliCommandsRefusesRunnerCommandsWithoutConfiguration(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for runner commands without a configuration")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == strings.Contains(recoveredErr.Error(), "no configuration") {
            t.Fatalf("expected the refusal to name the missing configuration, got %v", recoveredErr)
        }
    }()

    job := newRecordingCommand("job:orphan")

    NewModule(ModuleConfig{
        Runner: RunnerConfig{Commands: []clicontract.Command{job}},
    }).RegisterCliCommands(nil)
}

func TestModule_RegisterCliCommandsRefusesAFactoryThatReturnsNil(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for a configuration factory returning nil")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == strings.Contains(recoveredErr.Error(), "factory returned nil") {
            t.Fatalf("expected the refusal to name the nil factory result, got %v", recoveredErr)
        }
    }()

    NewModule(ModuleConfig{
        Configuration: func(kernelInstance kernelcontract.Kernel) *Configuration {
            return nil
        },
    }).RegisterCliCommands(nil)
}

func TestModule_RegisterCliCommandsBuildsTheConfigurationThroughTheFactory(t *testing.T) {
    factoryCalled := false
    module := NewModule(ModuleConfig{
        Configuration: func(kernelInstance kernelcontract.Kernel) *Configuration {
            factoryCalled = true

            return NewConfiguration()
        },
    })

    commands := module.RegisterCliCommands(nil)
    if false == factoryCalled {
        t.Fatal("expected the configuration factory to be used")
    }
    if 1 != len(commands) {
        t.Fatalf("expected one command from the factory configuration, got %d", len(commands))
    }
}

type configurationKernel struct {
    kernelcontract.Kernel
    configuration configcontract.Configuration
}

func (instance *configurationKernel) Config() configcontract.Configuration {
    return instance.configuration
}

func TestModule_RegisterCliCommandsRefusesTheRetiredLogsDirParameter(t *testing.T) {
    kernelInstance := &configurationKernel{configuration: newStubConfiguration(map[string]string{"melody.cron.logs_dir": "/var/log/app"})}

    defer func() {
        refusal, _ := recover().(*exception.Error)
        if nil == refusal || false == strings.Contains(refusal.Message(), "rename it to melody.cron.logs_directory") {
            t.Fatalf("expected the retired parameter refused, got %v", refusal)
        }
    }()

    NewModule(ModuleConfig{}).RegisterCliCommands(kernelInstance)
}

func TestModule_RegisterCliCommandsAdmitsTheSpelledOutParameter(t *testing.T) {
    kernelInstance := &configurationKernel{configuration: newStubConfiguration(map[string]string{ParameterLogsDirectory: "/var/log/app"})}

    if commands := NewModule(ModuleConfig{Configuration: staticConfiguration(NewConfiguration())}).RegisterCliCommands(kernelInstance); 1 != len(commands) {
        t.Fatalf("expected the generator registered, got %v", commands)
    }
}

func expectCronPanicNaming(t *testing.T, field string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError || field != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected a refusal naming %s, got %v", field, recoveredErr)
        }
    }()

    callback()
}

func TestModule_AFactoryAnsweringNilIsRefusedNamingTheField(t *testing.T) {
    expectCronPanicNaming(t, "ModuleConfig.Configuration", func() {
        NewModule(ModuleConfig{Configuration: staticConfiguration(nil)}).RegisterCliCommands(nil)
    })
}

func TestModule_TemplatesInstallACustomDialectAndReplaceABuiltinByName(t *testing.T) {
    callCount := 0
    replacement := &fakeTemplate{name: TemplateNameCrontab, content: "replaced\n", called: &callCount}

    commands := NewModule(ModuleConfig{
        Configuration: staticConfiguration(NewConfiguration()),
        Templates:     []Template{&fakeTemplate{name: "json-cron", content: "{}\n"}, replacement},
    }).RegisterCliCommands(nil)

    generateCommand := commands[0].(*GenerateCommand)

    if custom, lookupErr := generateCommand.resolveTemplate("json-cron"); nil != lookupErr || "json-cron" != custom.Name() {
        t.Fatalf("expected the custom dialect selectable by name, got %v", lookupErr)
    }

    if builtin, lookupErr := generateCommand.resolveTemplate(TemplateNameCrontab); nil != lookupErr || replacement != builtin {
        t.Fatalf("expected the template named as a builtin to replace it, got %T %v", builtin, lookupErr)
    }

    if k8s, lookupErr := generateCommand.resolveTemplate(TemplateNameK8s); nil != lookupErr || nil == k8s {
        t.Fatalf("expected the other builtin kept, got %v", lookupErr)
    }
}

func TestModule_ANilTemplateIsRefusedNamingItsIndex(t *testing.T) {
    expectCronPanicNaming(t, "ModuleConfig.Templates[1]", func() {
        NewModule(ModuleConfig{
            Configuration: staticConfiguration(NewConfiguration()),
            Templates:     []Template{&fakeTemplate{name: "json-cron"}, nil},
        }).RegisterCliCommands(nil)
    })
}

func TestModule_TemplatesWithoutAConfigurationAreRefused(t *testing.T) {
    expectCronPanicNaming(t, "ModuleConfig.Templates", func() {
        NewModule(ModuleConfig{Templates: []Template{&fakeTemplate{name: "json-cron"}}}).RegisterCliCommands(nil)
    })
}

func TestModule_AGeneratorDirectoryModeOutsideTheRuleIsRefusedNamingTheField(t *testing.T) {
    expectCronPanicNaming(t, "ModuleConfig.Generator.DirectoryMode", func() {
        NewModule(ModuleConfig{Generator: GeneratorConfig{DirectoryMode: 0o600}}).RegisterCliCommands(nil)
    })
}

func TestModule_TheGeneratorTakesTheConfiguredGenerator(t *testing.T) {
    commands := NewModule(ModuleConfig{
        Configuration: staticConfiguration(NewConfiguration()),
        Generator:     GeneratorConfig{DirectoryMode: 0o750, LogsDirectoryCheckDisabled: true},
    }).RegisterCliCommands(nil)

    generator := commands[0].(*GenerateCommand).generator
    if 0o750 != generator.directoryMode || false == generator.logsDirectoryCheckDisabled {
        t.Fatalf("expected the module's generator configuration handed to the command, got %v", generator)
    }
}

type spyBootWarningRegistrar struct {
    declared []string
    configs  []applicationcontract.BootWarningConfig
    added    int
}

func (instance *spyBootWarningRegistrar) DeclareBootWarning(name string, config applicationcontract.BootWarningConfig) {
    instance.declared = append(instance.declared, name)
    instance.configs = append(instance.configs, config)
}

func (instance *spyBootWarningRegistrar) AddBootWarning(warning applicationcontract.BootWarning) {
    instance.added++
}

func TestModule_DeclaresTheTickDedupWarningCodes(t *testing.T) {
    if "cron.tickDedup.noLocker" != TickDedupNoLockerBootWarningName || "cron.tickDedup.lockerNotSharedLease" != TickDedupLockerNotSharedLeaseBootWarningName || "cron.tickDedup.lockerAskFailed" != TickDedupLockerAskFailedBootWarningName {
        t.Fatalf("expected the codes cron.tickDedup.noLocker, cron.tickDedup.lockerNotSharedLease and cron.tickDedup.lockerAskFailed, got %s, %s and %s", TickDedupNoLockerBootWarningName, TickDedupLockerNotSharedLeaseBootWarningName, TickDedupLockerAskFailedBootWarningName)
    }

    registrar := &spyBootWarningRegistrar{}
    NewModule(ModuleConfig{}).RegisterBootWarnings(registrar)

    expected := []string{TickDedupNoLockerBootWarningName, TickDedupLockerNotSharedLeaseBootWarningName, TickDedupLockerAskFailedBootWarningName}
    if false == slices.Equal(expected, registrar.declared) {
        t.Fatalf("expected the three tick dedup codes declared, got %v", registrar.declared)
    }

    /* an application registering the runner without the module declares the same codes through its own BootWarningModule */
    alone := &spyBootWarningRegistrar{}
    DeclareRunnerBootWarnings(alone)
    if false == slices.Equal(expected, alone.declared) {
        t.Fatalf("expected the runner's codes declared without the module, got %v", alone.declared)
    }

    for _, config := range registrar.configs {
        if (applicationcontract.BootWarningConfig{}) != config {
            t.Fatalf("expected the codes declared with the zero config, got %+v", config)
        }
    }

    if 0 != registrar.added {
        t.Fatalf("expected no warning added at the boot, got %d", registrar.added)
    }
}
