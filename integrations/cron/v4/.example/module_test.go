package example

import (
    "bytes"
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"

    melodycli "github.com/precision-soft/melody/v4/cli"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

/* moduleTestApplicationName is the cli name the test configuration answers, the name the playbook's ownership line carries */
const moduleTestApplicationName = "billing"

type moduleTestCliConfiguration struct{}

func (instance *moduleTestCliConfiguration) Name() string {
    return moduleTestApplicationName
}

func (instance *moduleTestCliConfiguration) Description() string {
    return "billing"
}

/* moduleTestConfiguration answers the cli name and no parameter, so the generator reads its flags */
type moduleTestConfiguration struct{}

func (instance *moduleTestConfiguration) Get(name string) configcontract.Parameter {
    return nil
}

func (instance *moduleTestConfiguration) MustGet(name string) configcontract.Parameter {
    return nil
}

func (instance *moduleTestConfiguration) RegisterRuntime(name string, value any) {}

func (instance *moduleTestConfiguration) RegisterRuntimeSecret(name string, value any) {}

func (instance *moduleTestConfiguration) MarkSecret(name string) bool {
    return false
}

func (instance *moduleTestConfiguration) Resolve() error {
    return nil
}

func (instance *moduleTestConfiguration) Cli() configcontract.CliConfiguration {
    return &moduleTestCliConfiguration{}
}

func (instance *moduleTestConfiguration) Kernel() configcontract.KernelConfiguration {
    return nil
}

func (instance *moduleTestConfiguration) Http() configcontract.HttpConfiguration {
    return nil
}

func (instance *moduleTestConfiguration) Names() []string {
    return nil
}

/* the generator the module registers renders the ansible-cron dialect the module installs: the playbook opens with the dialect's own ownership line for the application, which only the template handed through ModuleConfig.Templates writes */
func TestNewCronModule_TheGeneratorRendersTheAnsibleCronDialectTheModuleInstalls(t *testing.T) {
    commands := NewCronModule().RegisterCliCommands(nil)
    if 1 != len(commands) || "melody:cron:generate" != commands[0].Name() {
        t.Fatalf("expected the generator from the cron module, got %v", commands)
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(melodyconfig.ServiceConfig, func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
        return &moduleTestConfiguration{}, nil
    }, containercontract.RegisterOptions{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    temporaryDirectory := t.TempDir()
    outputPath := filepath.Join(temporaryDirectory, "playbook.yml")

    var output bytes.Buffer
    runErr := melodycli.DispatchCommand(
        context.Background(),
        commands[0],
        runtimeInstance,
        []string{
            commands[0].Name(),
            "--template", "ansible-cron",
            "--out", outputPath,
            "--logs-directory", filepath.Join(temporaryDirectory, "logs"),
            "--binary", "/srv/app/bin/app",
            "--user", "deploy",
        },
        &output,
        melodycli.DispatchOptions{},
    )
    if nil != runErr {
        t.Fatalf("the generation failed: %v (%s)", runErr, output.String())
    }

    content, readErr := os.ReadFile(outputPath)
    if nil != readErr {
        t.Fatalf("read the playbook: %v", readErr)
    }

    firstLine := strings.SplitN(string(content), "\n", 2)[0]
    if ansibleCronOwnershipMarker+" for "+moduleTestApplicationName != firstLine {
        t.Fatalf("expected the playbook to open with the ansible-cron ownership line, got %q in:\n%s", firstLine, content)
    }

    if false == strings.Contains(string(content), "billing cron: ") {
        t.Fatalf("expected the module's task name prefix in the playbook, got:\n%s", content)
    }
}
