package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/internal/testhelper"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func newTestRuntime() *testRuntime {
	serviceContainer := container.NewContainer()
	scope := serviceContainer.NewScope()
	defer scope.Close()

	return &testRuntime{
		contextValue:   context.Background(),
		scopeValue:     scope,
		containerValue: serviceContainer,
	}
}

type testRuntime struct {
	contextValue   context.Context
	scopeValue     containercontract.Scope
	containerValue containercontract.Container
}

func (instance *testRuntime) Context() context.Context {
	return instance.contextValue
}

func (instance *testRuntime) Scope() containercontract.Scope {
	return instance.scopeValue
}

func (instance *testRuntime) Container() containercontract.Container {
	return instance.containerValue
}

var _ runtimecontract.Runtime = (*testRuntime)(nil)

type testCommand struct {
	nameValue        string
	descriptionValue string
	flagsValue       []clicontract.Flag
	runCallback      func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error
}

func (instance *testCommand) Name() string {
	return instance.nameValue
}

func (instance *testCommand) Description() string {
	return instance.descriptionValue
}

func (instance *testCommand) Flags() []clicontract.Flag {
	return instance.flagsValue
}

func (instance *testCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
	return instance.runCallback(runtimeInstance, commandContext)
}

func TestNewRootCommand_SetsNameAndUsage(t *testing.T) {
	rootCommand := NewCommandContext("app", "desc")

	if nil == rootCommand {
		t.Fatalf("expected rootCommand")
	}
	if "app" != rootCommand.Name {
		t.Fatalf("expected name %q, got %q", "app", rootCommand.Name)
	}
	if "desc" != rootCommand.Usage {
		t.Fatalf("expected usage %q, got %q", "desc", rootCommand.Usage)
	}
}

func TestRegister_PanicsOnNilRootCommand(t *testing.T) {
	runtimeInstance := newTestRuntime()

	command := &testCommand{
		nameValue:        "test",
		descriptionValue: "test",
		flagsValue:       nil,
		runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
			return nil
		},
	}

	testhelper.AssertPanics(t, func() {
		Register(nil, command, runtimeInstance)
	})
}

func TestRegister_PanicsOnNilCommand(t *testing.T) {
	runtimeInstance := newTestRuntime()

	rootCommand := NewCommandContext("app", "desc")

	testhelper.AssertPanics(t, func() {
		Register(rootCommand, nil, runtimeInstance)
	})
}

func TestRegister_PanicsOnNilRuntime(t *testing.T) {
	rootCommand := NewCommandContext("app", "desc")

	command := &testCommand{
		nameValue:        "test",
		descriptionValue: "test",
		flagsValue:       nil,
		runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
			return nil
		},
	}

	testhelper.AssertPanics(t, func() {
		Register(rootCommand, command, nil)
	})
}

func TestRegister_PanicsOnEmptyCommandName(t *testing.T) {
	runtimeInstance := newTestRuntime()
	rootCommand := NewCommandContext("app", "desc")

	command := &testCommand{
		nameValue:        "   ",
		descriptionValue: "test",
		flagsValue:       nil,
		runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
			return nil
		},
	}

	testhelper.AssertPanics(t, func() {
		Register(rootCommand, command, runtimeInstance)
	})
}

func TestRegister_AppendsCommandAndBindsFields(t *testing.T) {
	runtimeInstance := newTestRuntime()

	rootCommand := NewCommandContext("app", "desc")

	command := &testCommand{
		nameValue:        "hello",
		descriptionValue: "hello command",
		flagsValue: []clicontract.Flag{
			&clicontract.StringFlag{Name: "name"},
		},
		runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
			return nil
		},
	}

	if 0 != len(rootCommand.Commands) {
		t.Fatalf("expected empty commands")
	}

	Register(rootCommand, command, runtimeInstance)

	if 1 != len(rootCommand.Commands) {
		t.Fatalf("expected 1 command, got %d", len(rootCommand.Commands))
	}

	registered := rootCommand.Commands[0]

	if "hello" != strings.TrimSpace(registered.Name) {
		t.Fatalf("expected name %q, got %q", "hello", registered.Name)
	}
	if "hello command" != registered.Usage {
		t.Fatalf("expected usage %q, got %q", "hello command", registered.Usage)
	}
	if 1 != len(registered.Flags) {
		t.Fatalf("expected 1 flag, got %d", len(registered.Flags))
	}

	stringFlag, ok := registered.Flags[0].(*clicontract.StringFlag)
	if false == ok {
		t.Fatalf("expected *clicontract.StringFlag")
	}
	if "name" != stringFlag.Name {
		t.Fatalf("expected flag name %q, got %q", "name", stringFlag.Name)
	}
}

func TestRegister_PanicsOnDuplicateCommandName(t *testing.T) {
	runtimeInstance := newTestRuntime()
	rootCommand := NewCommandContext("app", "desc")

	commandA := &testCommand{
		nameValue:        "hello",
		descriptionValue: "a",
		flagsValue:       nil,
		runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
			return nil
		},
	}

	commandB := &testCommand{
		nameValue:        "  hello  ",
		descriptionValue: "b",
		flagsValue:       nil,
		runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
			return nil
		},
	}

	Register(rootCommand, commandA, runtimeInstance)

	testhelper.AssertPanics(t, func() {
		Register(rootCommand, commandB, runtimeInstance)
	})
}

func TestRegister_ActionCallsRunWithRuntimeInstance(t *testing.T) {
	runtimeInstance := newTestRuntime()

	rootCommand := NewCommandContext("app", "desc")

	expectedErr := errors.New("run error")

	var capturedRuntime runtimecontract.Runtime
	var capturedCommandContext *clicontract.CommandContext

	var commandInterface clicontract.Command

	commandImplementation := &testCommand{
		nameValue:        "hello",
		descriptionValue: "hello command",
		flagsValue:       nil,
		runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
			capturedRuntime = runtimeInstance
			capturedCommandContext = commandContext
			return expectedErr
		},
	}

	commandInterface = commandImplementation

	Register(rootCommand, commandInterface, runtimeInstance)

	commandInterface = nil

	registered := rootCommand.Commands[0]

	err := registered.Action(context.Background(), registered)
	if nil == err {
		t.Fatalf("expected error")
	}
	if expectedErr.Error() != err.Error() {
		t.Fatalf("expected %q, got %q", expectedErr.Error(), err.Error())
	}

	if runtimeInstance != capturedRuntime {
		t.Fatalf("expected runtime to be passed to Run")
	}
	if registered != capturedCommandContext {
		t.Fatalf("expected cli command to be passed to Run")
	}
}
