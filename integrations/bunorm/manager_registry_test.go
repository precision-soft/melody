package bunorm

import (
	"errors"
	"testing"

	"github.com/uptrace/bun"

	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type fakeLogger struct{}

func (instance *fakeLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Debug(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Info(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Error(message string, context loggingcontract.Context) {
}

func (instance *fakeLogger) Emergency(message string, context loggingcontract.Context) {
}

var _ loggingcontract.Logger = (*fakeLogger)(nil)

type fakeProvider struct {
	openCount int
}

func (instance *fakeProvider) Open(params ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
	instance.openCount = instance.openCount + 1
	return nil, nil
}

var _ Provider = (*fakeProvider)(nil)

func TestNewManagerRegistry_ErrorsWhenLoggerIsNil(t *testing.T) {
	_, registryErr := NewManagerRegistry(nil)
	if nil == registryErr {
		t.Fatalf("expected error")
	}

	if false == errors.Is(registryErr, ErrLoggerIsRequired) {
		t.Fatalf("expected ErrLoggerIsRequired")
	}
}

func TestNewManagerRegistry_ErrorsWhenNoProviderDefinitions(t *testing.T) {
	logger := &fakeLogger{}

	_, registryErr := NewManagerRegistry(logger)
	if nil == registryErr {
		t.Fatalf("expected error")
	}

	if false == errors.Is(registryErr, ErrNoProviderDefinitions) {
		t.Fatalf("expected ErrNoProviderDefinitions")
	}
}

func TestNewManagerRegistry_ErrorsWhenMultipleDefaults(t *testing.T) {
	logger := &fakeLogger{}

	providerA := &fakeProvider{}
	providerB := &fakeProvider{}

	_, registryErr := NewManagerRegistry(
		logger,
		ProviderDefinition{Name: "a", Provider: providerA, IsDefault: true},
		ProviderDefinition{Name: "b", Provider: providerB, IsDefault: true},
	)
	if nil == registryErr {
		t.Fatalf("expected error")
	}

	if false == errors.Is(registryErr, ErrMultipleDefaultProviderDefinitions) {
		t.Fatalf("expected ErrMultipleDefaultProviderDefinitions")
	}
}

func TestNewManagerRegistry_DefaultIsFirstWhenNoneIsDefault(t *testing.T) {
	logger := &fakeLogger{}

	providerA := &fakeProvider{}
	providerB := &fakeProvider{}

	registry, registryErr := NewManagerRegistry(
		logger,
		ProviderDefinition{Name: "a", Provider: providerA, IsDefault: false},
		ProviderDefinition{Name: "b", Provider: providerB, IsDefault: false},
	)
	if nil != registryErr {
		t.Fatalf("unexpected error: %v", registryErr)
	}

	defaultManager, managerErr := registry.DefaultManager()
	if nil != managerErr {
		t.Fatalf("unexpected error: %v", managerErr)
	}

	if "a" != defaultManager.DefinitionName() {
		t.Fatalf("expected default definition name to be 'a'")
	}
}

func TestManagerRegistry_CachesManagersOneToOne(t *testing.T) {
	logger := &fakeLogger{}

	providerA := &fakeProvider{}
	providerB := &fakeProvider{}

	registry, registryErr := NewManagerRegistry(
		logger,
		ProviderDefinition{Name: "a", Provider: providerA, IsDefault: true},
		ProviderDefinition{Name: "b", Provider: providerB, IsDefault: false},
	)
	if nil != registryErr {
		t.Fatalf("unexpected error: %v", registryErr)
	}

	managerA1, errA1 := registry.Manager("a")
	if nil != errA1 {
		t.Fatalf("unexpected error: %v", errA1)
	}

	managerA2, errA2 := registry.Manager("a")
	if nil != errA2 {
		t.Fatalf("unexpected error: %v", errA2)
	}

	if managerA1 != managerA2 {
		t.Fatalf("expected same manager instance for definition 'a'")
	}

	if 1 != providerA.openCount {
		t.Fatalf("expected provider 'a' to be opened once")
	}

	_, errB1 := registry.Manager("b")
	if nil != errB1 {
		t.Fatalf("unexpected error: %v", errB1)
	}

	if 1 != providerB.openCount {
		t.Fatalf("expected provider 'b' to be opened once")
	}
}
