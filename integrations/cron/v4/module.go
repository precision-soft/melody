package cron

import (
    "strconv"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/exception"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type ModuleConfig struct {
    /* Configuration builds the configuration that drives the generator and the runner against the booted kernel; a factory that answers nil is refused at registration as a wiring error. Left out, the module registers no commands, and Runner.Commands or Templates without it are refused at registration. */
    Configuration func(kernelInstance kernelcontract.Kernel) *Configuration

    /* Templates are installed in the generator after the builtin dialects, a template named as a builtin replacing it; a nil entry is refused naming its index. */
    Templates []Template

    /* KeyDefaults are the values the module's MELODY_CRON_* keys take when no .env file sets them. */
    KeyDefaults KeyDefaults

    /* Runner, when it lists Commands, adds the in-process melody:cron:run scheduler beside the generator, built by NewRunnerCommand from this configuration: an entry naming a command absent from Runner.Commands, or a value outside its rule, panics at boot. Without Commands the rest of Runner is not read. */
    Runner RunnerConfig

    /* Generator declares how melody:cron:generate writes; a DirectoryMode outside its rule is refused at registration naming the field. */
    Generator GeneratorConfig
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "cron"
}

func (instance *Module) Description() string {
    return "registers the crontab generation command and declares its MELODY_CRON_* keys"
}

func (instance *Module) RegisterEnvironmentKeys(registrar applicationcontract.EnvironmentKeyRegistrar) {
    for _, key := range environmentKeys(instance.config.KeyDefaults) {
        registrar.DeclareEnvironmentKey(key)
    }
}

func (instance *Module) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    /* the hook runs at every boot, after every parameter hook, whatever the process shape */
    if nil != kernelInstance {
        refuseRetiredParameters(kernelInstance.Config())
    }

    generator, generatorErr := resolveGeneratorConfig(instance.config.Generator)
    if nil != generatorErr {
        exception.Panic(exception.FromError(generatorErr))
    }

    for index, template := range instance.config.Templates {
        if nil == template {
            exception.Panic(
                exception.NewError(
                    "cron module template is nil",
                    map[string]any{"field": "ModuleConfig.Templates[" + strconv.Itoa(index) + "]"},
                    nil,
                ),
            )
        }
    }

    if nil == instance.config.Configuration {
        if 0 < len(instance.config.Runner.Commands) {
            exception.Panic(
                exception.NewError(
                    "cron module has runner commands but no configuration to schedule them from",
                    map[string]any{
                        "runnerCommandCount": len(instance.config.Runner.Commands),
                    },
                    nil,
                ),
            )
        }

        if 0 < len(instance.config.Templates) {
            exception.Panic(
                exception.NewError(
                    "cron module has templates but no configuration to generate from",
                    map[string]any{"field": "ModuleConfig.Templates", "requires": "ModuleConfig.Configuration"},
                    nil,
                ),
            )
        }

        return nil
    }

    configuration := instance.config.Configuration(kernelInstance)
    if nil == configuration {
        exception.Panic(
            exception.NewError("cron module configuration factory returned nil", map[string]any{"field": "ModuleConfig.Configuration"}, nil),
        )
    }

    commands := []clicontract.Command{newGenerateCommand(configuration, instance.config.Templates, generator)}

    if 0 < len(instance.config.Runner.Commands) {
        commands = append(commands, NewRunnerCommand(configuration, instance.config.Runner))
    }

    return commands
}

/* the codes are declared whatever the locker, which only Run reads, so MELODY_BOOT_WARNINGS_IGNORED may name them in every environment the module is registered in */
func (instance *Module) RegisterBootWarnings(registrar applicationcontract.BootWarningRegistrar) {
    DeclareRunnerBootWarnings(registrar)
}

/* DeclareRunnerBootWarnings declares the codes of the warnings the runner's Run writes; the module calls it, and an application that registers NewRunnerCommand without the module calls it from its own BootWarningModule, so MELODY_BOOT_WARNINGS_IGNORED may name them. */
func DeclareRunnerBootWarnings(registrar applicationcontract.BootWarningRegistrar) {
    registrar.DeclareBootWarning(TickDedupNoLockerBootWarningName, applicationcontract.BootWarningConfig{})
    registrar.DeclareBootWarning(TickDedupLockerNotSharedLeaseBootWarningName, applicationcontract.BootWarningConfig{})
    registrar.DeclareBootWarning(TickDedupLockerAskFailedBootWarningName, applicationcontract.BootWarningConfig{})
}

var (
    _ applicationcontract.Module               = (*Module)(nil)
    _ applicationcontract.EnvironmentKeyModule = (*Module)(nil)
    _ applicationcontract.CliModule            = (*Module)(nil)
    _ applicationcontract.BootWarningModule    = (*Module)(nil)
)
