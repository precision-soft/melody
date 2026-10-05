package container

import (
    "context"
    "fmt"
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

func WithoutTypeRegistration() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.AlsoRegisterType = false
    }
}

func WithTypeRegistration(isStrict bool) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.AlsoRegisterType = true
        option.TypeRegistrationIsStrict = isStrict
    }
}

/* WithCollectionPriority orders the registration inside AllImplementing collections: a higher priority is collected earlier. The unset priority is zero, and equal priorities keep the stable type-and-name order. Only a type-registered service can be collected. */
func WithCollectionPriority(priority int) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.CollectionPriority = priority
    }
}

/* Replacing admits a scoped registration whose name or registered type the container already holds; without it the collision is refused. Only the scoped registration paths read it. The waiver is remembered, so a later container registration of the same name is admitted, and it covers the registered type too unless the registration uses WithoutTypeRegistration. */
func Replacing() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.ReplacesContainerService = true
    }
}

/* WithTeardownDependency declares that this registration closes before the named container services, without resolving them, for a provider that captures a pre-built collaborator. Resolving is preferred wherever possible. Calls compose; the option orders teardown alone and takes no part in resolution cycle detection, and an edge to a service never created is dropped. An empty name and the service's own name are refused, and RegisterScoped refuses the option. Under ArmParallelTeardown a name nothing registered is refused with ErrTeardownDependencyWasNeverRegistered and a scoped one with ErrTeardownDependencyIsScoped. */
func WithTeardownDependency(serviceNames ...string) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.TeardownDependencyNames = append(option.TeardownDependencyNames, serviceNames...)
    }
}

/* WithTeardownDependencyOfType is WithTeardownDependency keyed by type; T and *T name the same node. A type nothing, or more than one service, is registered under orders nothing on the default path, and is refused under ArmParallelTeardown with ErrTeardownDependencyWasNeverRegistered or ErrTeardownDependencyTypeIsAmbiguous; a scoped-only type is refused with ErrTeardownDependencyIsScoped. A registration that files the type it declares against is refused with ErrTeardownDependencyIsSelf. */
func WithTeardownDependencyOfType[T any]() containercontract.RegisterOption {
    dependencyType := reflect.TypeOf((*T)(nil)).Elem()

    return func(option *containercontract.RegisterOptions) {
        option.TeardownDependencyTypes = append(option.TeardownDependencyTypes, dependencyType)
    }
}

/* WithoutTeardownReflection keeps the armed teardown's walk out of this service's memory, for a service whose own goroutines rewrite its pointer fields: the walk reads those words without synchronisation, which the race detector reports. The service's value is not walked where it is filed nor at ArmParallelTeardown, and a walk of another service that reaches it records the edge toward it and does not enter it, so what it holds orders nothing and is declared with WithTeardownDependency instead. The option also covers a value installed over the name, and the scoped registration paths do not read it. */
func WithoutTeardownReflection() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.SkipsTeardownReflection = true
    }
}

/* WithReplacesContainerService admits a scoped registration whose name or registered type the container already claims, so the scoped one answers inside a scope and the container's outside. Without it the overlap is refused. It admits substitution, not decoration. The container-level registration paths do not read it. */
func WithReplacesContainerService() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.ReplacesContainerService = true
    }
}

/* WithCloser closes the service through closer in place of the Close and CloseWithContext doors its value carries, handing it the teardown's remaining budget, the way a CloseWithContext door is handed it. It is the door for a value of a type the application does not own, whose own Close cannot be bounded: the closer bounds it, and the service keeps its type for every resolver. The closer replaces the value's doors at the container's teardown, for an instance an override evicted and for a value built after the container closed, and closes an override installed under the name when it takes the override's type; an override of a type it does not take closes through its own doors. A registration whose provider declares a type the closer does not take is refused with ErrCloserTypeMismatch, the scoped registrations refuse the option with ErrScopedCloserUnsupported, and a nil closer panics. */
func WithCloser[T any](closer func(closeContext context.Context, value T) error) containercontract.RegisterOption {
    if nil == closer {
        exception.Panic(exception.NewError("a closer is required", nil, nil))
    }

    return func(option *containercontract.RegisterOptions) {
        option.CloserValueType = reflect.TypeFor[T]()
        option.Closer = func(closeContext context.Context, value any) error {
            typedValue, takesValue := value.(T)
            if false == takesValue {
                return exception.NewError(
                    "the closer does not take the value it was handed",
                    map[string]any{
                        "closerValueType": reflect.TypeFor[T]().String(),
                        "valueType":       fmt.Sprintf("%T", value),
                    },
                    ErrCloserTypeMismatch,
                )
            }

            return closer(closeContext, typedValue)
        }
    }
}

func buildRegisterServiceOption() *containercontract.RegisterOptions {
    return &containercontract.RegisterOptions{
        AlsoRegisterType:         true,
        TypeRegistrationIsStrict: true,
    }
}

func applyRegisterServiceOptions(options []containercontract.RegisterOption) *containercontract.RegisterOptions {
    merged := buildRegisterServiceOption()
    for _, optionFunc := range options {
        if nil == optionFunc {
            continue
        }
        optionFunc(merged)
    }
    return merged
}
