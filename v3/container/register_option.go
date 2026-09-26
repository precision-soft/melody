package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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

/* WithReplacesContainerService admits a scoped registration whose name or registered type the container already claims, so the scoped one answers inside a scope and the container's outside. Without it the overlap is refused. It admits substitution, not decoration. The container-level registration paths do not read it. */
func WithReplacesContainerService() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.ReplacesContainerService = true
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
