package container

import (
	"testing"

	containercontract "github.com/precision-soft/melody/v2/container/contract"
)

type scopeTestService struct {
	value string
}

func TestScope_GetDelegatesToContainerAndCachesPerScope(t *testing.T) {
	serviceContainer := NewContainer()

	calls := 0

	err := serviceContainer.Register(
		"service.test",
		func(resolver containercontract.Resolver) (*scopeTestService, error) {
			calls++
			return &scopeTestService{value: "ok"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected register error: %v", err)
	}

	scope := serviceContainer.NewScope()

	_, err = scope.Get("service.test")
	if nil != err {
		t.Fatalf("unexpected get error: %v", err)
	}

	_, err = scope.Get("service.test")
	if nil != err {
		t.Fatalf("unexpected get error: %v", err)
	}

	if 1 != calls {
		t.Fatalf("expected provider to be called once per container singleton")
	}
}

func TestScope_OverrideInstance_IsolatedFromContainer(t *testing.T) {
	serviceContainer := NewContainer()

	err := serviceContainer.Register(
		"service.test",
		func(resolver containercontract.Resolver) (*scopeTestService, error) {
			return &scopeTestService{value: "container"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected register error: %v", err)
	}

	scope := serviceContainer.NewScope()

	err = scope.OverrideProtectedInstance(
		"service.test",
		&scopeTestService{value: "scope"},
	)
	if nil != err {
		t.Fatalf("unexpected override error: %v", err)
	}

	valueAny, err := scope.Get("service.test")
	if nil != err {
		t.Fatalf("unexpected get error: %v", err)
	}

	scopeValue := valueAny.(*scopeTestService)
	if "scope" != scopeValue.value {
		t.Fatalf("expected scope override value")
	}

	containerValueAny, err := serviceContainer.Get("service.test")
	if nil != err {
		t.Fatalf("unexpected container get error: %v", err)
	}

	containerValue := containerValueAny.(*scopeTestService)
	if "container" != containerValue.value {
		t.Fatalf("expected container value to remain unchanged")
	}
}

func TestScope_ClosePanicsOnGet(t *testing.T) {
	serviceContainer := NewContainer()

	scope := serviceContainer.NewScope()
	_ = scope.Close()

	defer func() {
		if nil == recover() {
			t.Fatalf("expected panic")
		}
	}()

	_, _ = scope.Get("service.test")
}

func TestScope_HasReturnsFalseWhenClosed(t *testing.T) {
	serviceContainer := NewContainer()

	scope := serviceContainer.NewScope()
	_ = scope.Close()

	if true == scope.Has("a") {
		t.Fatalf("expected false")
	}
}
