package container

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/exception"
)

type testService struct {
	Value string
}

type testInterface interface {
	Name() string
}

type testImplementation struct {
	name string
}

func (instance *testImplementation) Name() string {
	return instance.name
}

func TestContainer_RegisterAndGetService(t *testing.T) {
	serviceContainer := NewContainer()

	err := serviceContainer.Register(
		"service.test",
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{Value: "ok"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected error")
	}

	valueAny, err := serviceContainer.Get("service.test")
	if nil != err {
		t.Fatalf("unexpected get error: %v", err)
	}

	service := valueAny.(*testService)
	if "ok" != service.Value {
		t.Fatalf("unexpected value")
	}
}

func TestContainer_Register_ReturnsErrorOnInvalidArguments(t *testing.T) {
	serviceContainer := NewContainer()

	err := serviceContainer.Register(
		"",
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{}, nil
		},
	)
	if nil == err {
		t.Fatalf("expected error")
	}
}

func TestContainer_MustRegister_PanicsOnInvalidArguments(t *testing.T) {
	serviceContainer := NewContainer()

	defer func() {
		if nil == recover() {
			t.Fatalf("expected panic")
		}
	}()

	serviceContainer.MustRegister(
		"",
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{}, nil
		},
	)
}

func TestContainer_RegisterType_AndResolveByType(t *testing.T) {
	serviceContainer := NewContainer()

	err := RegisterType[*testService](
		serviceContainer,
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{Value: "typed"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected error")
	}

	service := MustFromResolverByType[*testService](serviceContainer)
	if "typed" != service.Value {
		t.Fatalf("unexpected value")
	}
}

func TestContainer_RegisterType_Interface_AndResolveByType(t *testing.T) {
	serviceContainer := NewContainer()

	err := RegisterType[testInterface](
		serviceContainer,
		func(resolver containercontract.Resolver) (testInterface, error) {
			return &testImplementation{name: "impl"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected error")
	}

	value := MustFromResolverByType[testInterface](serviceContainer)
	if "impl" != value.Name() {
		t.Fatalf("unexpected name")
	}
}

func TestContainer_MustFromResolver_PanicsWhenMissing(t *testing.T) {
	serviceContainer := NewContainer()

	defer func() {
		if nil == recover() {
			t.Fatalf("expected panic")
		}
	}()

	_ = MustFromResolver[*testService](serviceContainer, "service.missing")
}

func TestContainer_SingletonInstantiation(t *testing.T) {
	serviceContainer := NewContainer()

	calls := 0

	err := serviceContainer.Register(
		"service.singleton",
		func(resolver containercontract.Resolver) (*testService, error) {
			calls++
			return &testService{Value: "ok"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected error")
	}

	_, err = serviceContainer.Get("service.singleton")
	if nil != err {
		t.Fatalf("unexpected error")
	}

	_, err = serviceContainer.Get("service.singleton")
	if nil != err {
		t.Fatalf("unexpected error")
	}

	if 1 != calls {
		t.Fatalf("expected factory to be called once")
	}
}

func TestContainer_FactoryErrorIsPropagated(t *testing.T) {
	serviceContainer := NewContainer()

	expectedErr := errors.New("factory error")

	err := serviceContainer.Register(
		"service.fail",
		func(resolver containercontract.Resolver) (*testService, error) {
			return nil, expectedErr
		},
	)
	if nil != err {
		t.Fatalf("unexpected register error")
	}

	_, err = serviceContainer.Get("service.fail")
	if nil == err {
		t.Fatalf("expected get error")
	}
}

func TestContainer_Has(t *testing.T) {
	serviceContainer := NewContainer()

	if true == serviceContainer.Has("a") {
		t.Fatalf("expected false")
	}

	_ = serviceContainer.Register(
		"a",
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{}, nil
		},
	)

	if false == serviceContainer.Has("a") {
		t.Fatalf("expected true")
	}
}

func TestContainer_OverrideInstance(t *testing.T) {
	serviceContainer := NewContainer()

	err := serviceContainer.Register(
		"test.service",
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{Value: "original"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected error")
	}

	err = serviceContainer.OverrideInstance(
		"test.service",
		&testService{Value: "override"},
	)
	if nil != err {
		t.Fatalf("unexpected override error: %v", err)
	}

	valueAny, err := serviceContainer.Get("test.service")
	if nil != err {
		t.Fatalf("unexpected get error: %v", err)
	}

	service := valueAny.(*testService)
	if "override" != service.Value {
		t.Fatalf("expected override to win")
	}
}

func TestContainer_OverrideInstance_OverridesRegisteredTypeMappings(t *testing.T) {
	serviceContainer := NewContainer()

	err := serviceContainer.Register(
		"service.interface",
		func(resolver containercontract.Resolver) (testInterface, error) {
			return &testImplementation{name: "original"}, nil
		},
		WithTypeRegistration(true),
	)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	err = serviceContainer.OverrideProtectedInstance(
		"service.interface",
		&testImplementation{name: "override"},
	)
	if nil != err {
		t.Fatalf("unexpected override error: %v", err)
	}

	resolved := MustFromResolverByType[testInterface](serviceContainer)
	if "override" != resolved.Name() {
		t.Fatalf("expected override to win for type resolution")
	}
}

func TestContainer_GetByType_ReturnsCompleteConflictReport(t *testing.T) {
	serviceContainer := NewContainer()

	err := serviceContainer.Register(
		"service.a",
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{Value: "a"}, nil
		},
		WithTypeRegistration(false),
	)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	err = serviceContainer.Register(
		"service.b",
		func(resolver containercontract.Resolver) (*testService, error) {
			return &testService{Value: "b"}, nil
		},
		WithTypeRegistration(false),
	)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = serviceContainer.GetByType(reflect.TypeOf((*testService)(nil)))
	if nil == err {
		t.Fatalf("expected error")
	}

	var conflicts []string

	currentErr := err
	for nil != currentErr {
		melodyError, ok := currentErr.(*exception.Error)
		if true == ok {
			context := melodyError.Context()
			conflictsAny, exists := context["conflicts"]
			if true == exists {
				typedConflicts, ok := conflictsAny.([]string)
				if false == ok {
					t.Fatalf("expected conflicts to be []string")
				}
				conflicts = typedConflicts
				break
			}
		}

		currentErr = errors.Unwrap(currentErr)
	}

	if nil == conflicts {
		t.Fatalf("expected conflicts in error chain")
	}

	if 2 != len(conflicts) {
		t.Fatalf("expected 2 conflicts")
	}

	foundA := false
	foundB := false

	for _, conflictName := range conflicts {
		if "service.a" == conflictName {
			foundA = true
		}
		if "service.b" == conflictName {
			foundB = true
		}
	}

	if false == foundA {
		t.Fatalf("expected conflicts to include service.a")
	}
	if false == foundB {
		t.Fatalf("expected conflicts to include service.b")
	}
}

func TestContainer_ConcurrentGet_SingleFactoryCall(t *testing.T) {
	serviceContainer := NewContainer()

	var calls atomic.Int64

	err := serviceContainer.Register(
		"service.concurrent",
		func(resolver containercontract.Resolver) (*testService, error) {
			calls.Add(1)
			return &testService{Value: "ok"}, nil
		},
	)
	if nil != err {
		t.Fatalf("unexpected error")
	}

	errorChannel := make(chan error, 32)

	var waitGroup sync.WaitGroup
	waitGroup.Add(32)

	for i := 0; i < 32; i++ {
		go func() {
			defer waitGroup.Done()

			_, err := serviceContainer.Get("service.concurrent")
			if nil != err {
				errorChannel <- err
			}
		}()
	}

	waitGroup.Wait()
	close(errorChannel)

	for err := range errorChannel {
		if nil != err {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if 1 != calls.Load() {
		t.Fatalf("expected factory to be called once")
	}
}
