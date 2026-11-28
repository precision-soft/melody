package application

import (
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* builtAtBootCauseChainDepth is the depth of the cause chain a boot warning carries, the default depth of the exit line */
const builtAtBootCauseChainDepth = 8

/* builtAtBootWiringRefusals are the container's own refusals: a provider that reaches one is a wiring mistake, which fails the boot whatever the mode declares */
var builtAtBootWiringRefusals = []error{
    container.ErrServiceNotRegistered,
    container.ErrServiceTypeAmbiguous,
    container.ErrCloseDoorRequired,
    container.ErrCircularDependency,
    container.ErrScopedServiceAtBoot,
    containercontract.ErrRuntimeKindRefused,
    container.ErrServiceTypeMismatch,
    container.ErrProviderReturnedNil,
}

func (instance *Application) buildServicesBuiltAtBoot(bootRuntime containercontract.Runtime) {
    lister, lists := instance.kernel.ServiceContainer().(interface {
        BuiltAtBootRegistrations() []containercontract.BuiltAtBootRegistration
    })
    if false == lists {
        return
    }

    /* a command is one caller: it builds a service that warns where it uses it, under its run */
    buildsWarningServices := config.ModeCli != instance.runtimeFlags.Mode()

    for _, registration := range lister.BuiltAtBootRegistrations() {
        if containercontract.BootFailureWarns == registration.BootFailure && false == buildsWarningServices {
            continue
        }

        _, buildErr := runtime.FromRuntime[any](bootRuntime, registration.ServiceName)
        if nil == buildErr {
            continue
        }

        if true == builtAtBootFailureFailsBoot(bootRuntime, registration, buildErr) {
            exception.Panic(
                exception.NewError(
                    "a service built at boot cannot be built",
                    exceptioncontract.Context{
                        "serviceName": registration.ServiceName,
                        "bootFailure": registration.BootFailure.String(),
                    },
                    buildErr,
                ),
            )
        }

        instance.bootWarnings = append(instance.bootWarnings, internal.BootWarning{
            Name:    internal.BootWarningNameServiceBuiltAtBootFailed,
            Message: "a service built at boot could not be built: the boot goes on, the first resolution of the service builds it again and answers the failure if it still cannot be built",
            Context: loggingcontract.Context{
                "serviceName": registration.ServiceName,
                "cause":       builtAtBootCauseOf(buildErr),
            },
        })
    }
}

/* a boot whose own context ended is a process stopping, not a dependency to warn about */
func builtAtBootFailureFailsBoot(bootRuntime containercontract.Runtime, registration containercontract.BuiltAtBootRegistration, buildErr error) bool {
    if nil != bootRuntime.Context().Err() {
        return true
    }

    if containercontract.BootFailureFailsBoot == registration.BootFailure {
        return true
    }

    for _, wiringRefusal := range builtAtBootWiringRefusals {
        if true == exception.ChainIs(buildErr, wiringRefusal) {
            return true
        }
    }

    return false
}

/* builtAtBootCauseOf renders the failure and its causes on one line, as the exit line does: the first link, then ": cause N: " before each next one */
func builtAtBootCauseOf(buildErr error) string {
    links := exception.BuildCauseChain(buildErr, builtAtBootCauseChainDepth)
    if 0 == len(links) {
        return ""
    }

    return internal.AppendCauseChain(internal.BoundDiagnosticText(internal.EscapeControlCharacters(links[0])), links[1:], builtAtBootCauseChainDepth)
}
