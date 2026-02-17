package container

import (
	"errors"
	"reflect"
	"testing"

	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/exception"
)

type resolverTestService struct {
	value string
}

type resolverTestResolver struct {
	servicesByName map[string]any
	servicesByType map[reflect.Type]any
}

func (instance *resolverTestResolver) Get(serviceName string) (any, error) {
	value, exists := instance.servicesByName[serviceName]
	if false == exists {
		return nil, errors.New("service missing")
	}

	return value, nil
}

func (instance *resolverTestResolver) MustGet(serviceName string) any {
	value, err := instance.Get(serviceName)
	if nil != err {
		exception.Panic(
			exception.FromError(err),
		)
	}

	return value
}

func (instance *resolverTestResolver) GetByType(targetType reflect.Type) (any, error) {
	value, exists := instance.servicesByType[targetType]
	if false == exists {
		return nil, errors.New("service missing")
	}

	return value, nil
}

func (instance *resolverTestResolver) MustGetByType(targetType reflect.Type) any {
	value, err := instance.GetByType(targetType)
	if nil != err {
		exception.Panic(
			exception.FromError(err),
		)
	}

	return value
}

func (instance *resolverTestResolver) Has(serviceName string) bool {
	_, exists := instance.servicesByName[serviceName]

	return true == exists
}

func (instance *resolverTestResolver) HasType(targetType reflect.Type) bool {
	_, exists := instance.servicesByType[targetType]

	return true == exists
}

func TestFromResolver_HappyPath(t *testing.T) {
	resolver := &resolverTestResolver{
		servicesByName: map[string]any{
			"service.test": &resolverTestService{value: "ok"},
		},
		servicesByType: map[reflect.Type]any{},
	}

	value, err := FromResolver[*resolverTestService](resolver, "service.test")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	if "ok" != value.value {
		t.Fatalf("unexpected value: %s", value.value)
	}
}

func TestFromResolver_MissingService_ReturnsError(t *testing.T) {
	resolver := &resolverTestResolver{
		servicesByName: map[string]any{},
		servicesByType: map[reflect.Type]any{},
	}

	_, err := FromResolver[*resolverTestService](resolver, "service.missing")
	if nil == err {
		t.Fatalf("expected error")
	}

	typedError, ok := err.(*exception.Error)
	if false == ok {
		t.Fatalf("expected *exception.Error, got: %T", err)
	}

	if "service not registered in resolver" != typedError.Message() {
		t.Fatalf("unexpected error message: %s", typedError.Message())
	}
}

func TestFromResolver_TypeMismatch(t *testing.T) {
	resolver := &resolverTestResolver{
		servicesByName: map[string]any{
			"service.test": "not a service",
		},
		servicesByType: map[reflect.Type]any{},
	}

	_, err := FromResolver[*resolverTestService](resolver, "service.test")
	if nil == err {
		t.Fatalf("expected error")
	}

	typedError, ok := err.(*exception.Error)
	if false == ok {
		t.Fatalf("expected *exception.Error, got: %T", err)
	}

	if "service has wrong type" != typedError.Message() {
		t.Fatalf("unexpected error message: %s", typedError.Message())
	}
}

func TestMustFromResolver_PanicsOnError(t *testing.T) {
	resolver := &resolverTestResolver{
		servicesByName: map[string]any{},
		servicesByType: map[reflect.Type]any{},
	}

	defer func() {
		recoveredValue := recover()
		if nil == recoveredValue {
			t.Fatalf("expected panic")
		}
	}()

	_ = MustFromResolver[*resolverTestService](resolver, "service.missing")
}

func TestFromResolverByType_HappyPath(t *testing.T) {
	targetType := reflect.TypeOf(&resolverTestService{})
	resolver := &resolverTestResolver{
		servicesByName: map[string]any{},
		servicesByType: map[reflect.Type]any{
			targetType: &resolverTestService{value: "ok"},
		},
	}

	value, err := FromResolverByType[*resolverTestService](resolver)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}

	if "ok" != value.value {
		t.Fatalf("unexpected value: %s", value.value)
	}
}

func TestFromResolverByType_MissingService_ReturnsError(t *testing.T) {
	resolver := &resolverTestResolver{
		servicesByName: map[string]any{},
		servicesByType: map[reflect.Type]any{},
	}

	_, err := FromResolverByType[*resolverTestService](resolver)
	if nil == err {
		t.Fatalf("expected error")
	}
}

func TestFromResolverByType_TypeMismatch(t *testing.T) {
	targetType := reflect.TypeOf(&resolverTestService{})
	resolver := &resolverTestResolver{
		servicesByName: map[string]any{},
		servicesByType: map[reflect.Type]any{
			targetType: "not a service",
		},
	}

	_, err := FromResolverByType[*resolverTestService](resolver)
	if nil == err {
		t.Fatalf("expected error")
	}

	typedError, ok := err.(*exception.Error)
	if false == ok {
		t.Fatalf("expected *exception.Error, got: %T", err)
	}

	if "resolved service has unexpected type" != typedError.Message() {
		t.Fatalf("unexpected error message: %s", typedError.Message())
	}
}

var _ containercontract.Resolver = (*resolverTestResolver)(nil)
