package bunorm

import (
	"errors"
	"reflect"
	"testing"

	"github.com/uptrace/bun"

	containercontract "github.com/precision-soft/melody/container/contract"
)

type fakeResolver struct{}

func (instance *fakeResolver) Get(serviceName string) (any, error) {
	return nil, errors.New("not implemented")
}

func (instance *fakeResolver) MustGet(serviceName string) any {
	panic("not implemented")
}

func (instance *fakeResolver) GetByType(targetType reflect.Type) (any, error) {
	return nil, errors.New("not implemented")
}

func (instance *fakeResolver) MustGetByType(targetType reflect.Type) any {
	panic("not implemented")
}

func (instance *fakeResolver) Has(serviceName string) bool {
	return false
}

func (instance *fakeResolver) HasType(targetType reflect.Type) bool {
	return false
}

var _ containercontract.Resolver = (*fakeResolver)(nil)

type fakeProvider struct {
	openCount int
}

func (instance *fakeProvider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
	instance.openCount = instance.openCount + 1
	return nil, nil
}

func TestNewManagerRegistry_ErrorsWhenResolverIsNil(t *testing.T) {
	_, registryErr := NewManagerRegistry(nil)
	if nil == registryErr {
		t.Fatalf("expected error")
	}

	if false == errors.Is(registryErr, ErrResolverIsRequired) {
		t.Fatalf("expected ErrResolverIsRequired")
	}
}

func TestNewManagerRegistry_ErrorsWhenNoProviderDefinitions(t *testing.T) {
	resolver := &fakeResolver{}

	_, registryErr := NewManagerRegistry(resolver)
	if nil == registryErr {
		t.Fatalf("expected error")
	}

	if false == errors.Is(registryErr, ErrNoProviderDefinitions) {
		t.Fatalf("expected ErrNoProviderDefinitions")
	}
}

func TestNewManagerRegistry_ErrorsWhenMultipleDefaults(t *testing.T) {
	resolver := &fakeResolver{}

	providerA := &fakeProvider{}
	providerB := &fakeProvider{}

	_, registryErr := NewManagerRegistry(
		resolver,
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
	resolver := &fakeResolver{}

	providerA := &fakeProvider{}
	providerB := &fakeProvider{}

	registry, registryErr := NewManagerRegistry(
		resolver,
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
	resolver := &fakeResolver{}

	providerA := &fakeProvider{}
	providerB := &fakeProvider{}

	registry, registryErr := NewManagerRegistry(
		resolver,
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
