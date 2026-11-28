package runtime_test

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/application"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/mailer"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
    "github.com/precision-soft/melody/v4/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
    "github.com/precision-soft/melody/v4/translation"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
    "github.com/precision-soft/melody/v4/validation"
)

type twinClock struct{ clockcontract.Clock }
type twinLocker struct{ lockcontract.Locker }
type twinDispatcher struct{ eventcontract.EventDispatcher }
type twinConfiguration struct{ configcontract.Configuration }
type twinTranslator struct{ translationcontract.Translator }
type twinBus struct{ messagebuscontract.Bus }
type twinMailer struct{ mailercontract.Mailer }
type twinFirewallManager struct{ securitycontract.FirewallManager }
type twinSessionManager struct{ sessioncontract.Manager }
type twinRouteRegistry struct{ httpcontract.RouteRegistry }
type twinUrlGenerator struct{ httpcontract.UrlGenerator }
type twinRouter struct{ httpcontract.Router }

type softForm string

const (
    softFormPanic softForm = "panic"
    softFormNil   softForm = "nil"
    softFormError softForm = "error"
)

type accessorTwinCase struct {
    twinName    string
    serviceName string
    first       any
    second      any
    register    func(registrar containercontract.Registrar, value any, seen *any)
    twin        func(runtimeInstance containercontract.Runtime) (any, error)
    sibling     func(ctx context.Context, resolver containercontract.Resolver) any
    form        softForm
}

func registerAs[T any](serviceName string) func(registrar containercontract.Registrar, value any, seen *any) {
    return func(registrar containercontract.Registrar, value any, seen *any) {
        typed := value.(T)
        container.MustRegister[T](registrar, serviceName, func(resolver containercontract.Resolver) (T, error) {
            *seen = resolver.Runtime().Context().Value(runtimeContextKey{})

            return typed, nil
        }, containercontract.RegisterOptions{})
    }
}

func mustTwin[T any](accessor func(containercontract.Runtime) T) func(containercontract.Runtime) (any, error) {
    return func(runtimeInstance containercontract.Runtime) (any, error) {
        return accessor(runtimeInstance), nil
    }
}

func siblingOf[T any](accessor func(context.Context, containercontract.Resolver) T) func(context.Context, containercontract.Resolver) any {
    return func(ctx context.Context, resolver containercontract.Resolver) any {
        return accessor(ctx, resolver)
    }
}

func accessorTwinCases() []accessorTwinCase {
    return []accessorTwinCase{
        {"ValidatorMustFromRuntime", validation.ServiceValidator, validation.NewValidator(validation.ValidatorConfig{}), validation.NewValidator(validation.ValidatorConfig{}), registerAs[*validation.Validator](validation.ServiceValidator), mustTwin(validation.ValidatorMustFromRuntime), siblingOf(validation.ValidatorMustFromResolver), softFormPanic},
        {"ValidatorFromRuntime", validation.ServiceValidator, validation.NewValidator(validation.ValidatorConfig{}), validation.NewValidator(validation.ValidatorConfig{}), registerAs[*validation.Validator](validation.ServiceValidator), func(runtimeInstance containercontract.Runtime) (any, error) {
            if validatorInstance := validation.ValidatorFromRuntime(runtimeInstance); nil != validatorInstance {
                return validatorInstance, nil
            }

            return nil, nil
        }, siblingOf(validation.ValidatorFromResolver), softFormNil},
        {"ClockMustFromRuntime", clock.ServiceClock, &twinClock{}, &twinClock{}, registerAs[clockcontract.Clock](clock.ServiceClock), mustTwin(clock.ClockMustFromRuntime), siblingOf(clock.ClockMustFromResolver), softFormPanic},
        {"LockerMustFromRuntime", lock.ServiceLocker, &twinLocker{}, &twinLocker{}, registerAs[lockcontract.Locker](lock.ServiceLocker), mustTwin(lock.LockerMustFromRuntime), siblingOf(lock.LockerMustFromResolver), softFormPanic},
        {"EventDispatcherMustFromRuntime", event.ServiceEventDispatcher, &twinDispatcher{}, &twinDispatcher{}, registerAs[eventcontract.EventDispatcher](event.ServiceEventDispatcher), mustTwin(event.EventDispatcherMustFromRuntime), siblingOf(event.EventDispatcherMustFromResolver), softFormPanic},
        {"ConfigFromRuntime", config.ServiceConfig, &twinConfiguration{}, &twinConfiguration{}, registerAs[configcontract.Configuration](config.ServiceConfig), func(runtimeInstance containercontract.Runtime) (any, error) {
            configuration, err := config.ConfigFromRuntime(runtimeInstance)
            if nil != err {
                return nil, err
            }

            return configuration, nil
        }, func(ctx context.Context, resolver containercontract.Resolver) any {
            configuration, _ := config.ConfigFromResolver(ctx, resolver)

            return configuration
        }, softFormError},
        {"ConfigMustFromRuntime", config.ServiceConfig, &twinConfiguration{}, &twinConfiguration{}, registerAs[configcontract.Configuration](config.ServiceConfig), mustTwin(config.ConfigMustFromRuntime), siblingOf(config.ConfigMustFromResolver), softFormPanic},
        {"TranslatorMustFromRuntime", translation.ServiceTranslator, &twinTranslator{}, &twinTranslator{}, registerAs[translationcontract.Translator](translation.ServiceTranslator), mustTwin(translation.TranslatorMustFromRuntime), siblingOf(translation.TranslatorMustFromResolver), softFormPanic},
        {"ProcessRoleMustFromRuntime", application.ServiceProcessRole, config.RoleWeb, config.RoleWorker, registerAs[string](application.ServiceProcessRole), mustTwin(application.ProcessRoleMustFromRuntime), siblingOf(application.ProcessRoleMustFromResolver), softFormPanic},
        {"BusMustFromRuntime", messagebus.ServiceBus, &twinBus{}, &twinBus{}, registerAs[messagebuscontract.Bus](messagebus.ServiceBus), mustTwin(messagebus.BusMustFromRuntime), siblingOf(messagebus.BusMustFromResolver), softFormPanic},
        {"MailerMustFromRuntime", mailer.ServiceMailer, &twinMailer{}, &twinMailer{}, registerAs[mailercontract.Mailer](mailer.ServiceMailer), mustTwin(mailer.MailerMustFromRuntime), siblingOf(mailer.MailerMustFromResolver), softFormPanic},
        {"FirewallManagerMustFromRuntime", security.ServiceFirewallManager, &twinFirewallManager{}, &twinFirewallManager{}, registerAs[securitycontract.FirewallManager](security.ServiceFirewallManager), mustTwin(security.FirewallManagerMustFromRuntime), siblingOf(security.FirewallManagerMustFromResolver), softFormPanic},
        {"FirewallManagerFromRuntime", security.ServiceFirewallManager, &twinFirewallManager{}, &twinFirewallManager{}, registerAs[securitycontract.FirewallManager](security.ServiceFirewallManager), func(runtimeInstance containercontract.Runtime) (any, error) {
            if firewallManager := security.FirewallManagerFromRuntime(runtimeInstance); nil != firewallManager {
                return firewallManager, nil
            }

            return nil, nil
        }, siblingOf(security.FirewallManagerFromResolver), softFormNil},
        {"SessionMustFromRuntime", session.ServiceSessionManager, &twinSessionManager{}, &twinSessionManager{}, registerAs[sessioncontract.Manager](session.ServiceSessionManager), mustTwin(session.SessionMustFromRuntime), siblingOf(session.SessionMustFromResolver), softFormPanic},
        {"RouteRegistryMustFromRuntime", http.ServiceRouteRegistry, &twinRouteRegistry{}, &twinRouteRegistry{}, registerAs[httpcontract.RouteRegistry](http.ServiceRouteRegistry), mustTwin(http.RouteRegistryMustFromRuntime), siblingOf(http.RouteRegistryMustFromResolver), softFormPanic},
        {"UrlGeneratorMustFromRuntime", http.ServiceUrlGenerator, &twinUrlGenerator{}, &twinUrlGenerator{}, registerAs[httpcontract.UrlGenerator](http.ServiceUrlGenerator), mustTwin(http.UrlGeneratorMustFromRuntime), siblingOf(http.UrlGeneratorMustFromResolver), softFormPanic},
        {"RouterMustFromRuntime", http.ServiceRouter, &twinRouter{}, &twinRouter{}, registerAs[httpcontract.Router](http.ServiceRouter), mustTwin(http.RouterMustFromRuntime), siblingOf(http.RouterMustFromResolver), softFormPanic},
    }
}

func callTwin(testCase accessorTwinCase, runtimeInstance containercontract.Runtime) (value any, err error, recovered any) {
    defer func() {
        recovered = recover()
    }()

    value, err = testCase.twin(runtimeInstance)

    return value, err, nil
}

func TestAccessorTwin_AnswersTheSiblingsInstanceForTheSameRuntime(t *testing.T) {
    for _, testCase := range accessorTwinCases() {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        seen := any(nil)
        testCase.register(serviceContainer, testCase.first, &seen)

        runtimeInstance := runtime.NewRequest(context.WithValue(t.Context(), runtimeContextKey{}, testCase.twinName), serviceContainer.NewScope())

        value, err, recovered := callTwin(testCase, runtimeInstance)
        if nil != err || nil != recovered {
            t.Fatalf("%s: unexpected failure %v / %v", testCase.twinName, err, recovered)
        }

        if testCase.twinName != seen {
            t.Fatalf("%s: expected the provider to run under the runtime's context, read %v", testCase.twinName, seen)
        }

        if testCase.first != value || testCase.sibling(runtimeInstance.Context(), runtimeInstance.Scope()) != value {
            t.Fatalf("%s: expected the sibling's instance, got %v", testCase.twinName, value)
        }
    }
}

func TestAccessorTwin_ResolvesThroughTheRuntimesScope(t *testing.T) {
    for _, testCase := range accessorTwinCases() {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        seen := any(nil)
        testCase.register(serviceContainer, testCase.first, &seen)

        scope := serviceContainer.NewScope()
        if overrideErr := scope.OverrideProtectedInstance(testCase.serviceName, testCase.second, containercontract.OverrideOptions{}); nil != overrideErr {
            t.Fatalf("%s: override: %v", testCase.twinName, overrideErr)
        }

        value, err, recovered := callTwin(testCase, runtime.NewRun(t.Context(), scope))
        if nil != err || nil != recovered {
            t.Fatalf("%s: unexpected failure %v / %v", testCase.twinName, err, recovered)
        }

        if testCase.second != value {
            t.Fatalf("%s: expected the scope's override, got %v", testCase.twinName, value)
        }
    }
}

func assertTwinRefusal(t *testing.T, testCase accessorTwinCase, value any, err error, recovered any, expectedText string) {
    t.Helper()

    switch testCase.form {
    case softFormPanic:
        if nil == recovered || false == strings.Contains(recoveredText(recovered), expectedText) {
            t.Fatalf("%s: expected a panic naming %q, got %v", testCase.twinName, expectedText, recovered)
        }
    case softFormNil:
        if nil != recovered || nil != err || nil != value {
            t.Fatalf("%s: expected nil, got %v / %v / %v", testCase.twinName, value, err, recovered)
        }
    case softFormError:
        if nil != recovered || nil == err || false == strings.Contains(err.Error()+recoveredText(err), expectedText) {
            t.Fatalf("%s: expected an error naming %q, got %v / %v", testCase.twinName, expectedText, err, recovered)
        }
    }
}

func TestAccessorTwin_RefusesANilRuntime(t *testing.T) {
    for _, testCase := range accessorTwinCases() {
        value, err, recovered := callTwin(testCase, nil)

        assertTwinRefusal(t, testCase, value, err, recovered, "runtime may not be nil")
    }
}

func TestAccessorTwin_RefusesAnUnregisteredServiceNamingIt(t *testing.T) {
    for _, testCase := range accessorTwinCases() {
        serviceContainer := container.NewContainer(container.ContainerConfig{})

        value, err, recovered := callTwin(testCase, runtime.NewRequest(t.Context(), serviceContainer.NewScope()))

        assertTwinRefusal(t, testCase, value, err, recovered, testCase.serviceName)
    }
}

func recoveredText(value any) string {
    builder := strings.Builder{}

    current, isError := value.(error)
    if false == isError {
        return fmt.Sprint(value)
    }

    for nil != current {
        builder.WriteString(current.Error())

        if contextual, ok := current.(interface{ Context() exceptioncontract.Context }); true == ok {
            builder.WriteString(fmt.Sprint(contextual.Context()))
        }

        builder.WriteString("\n")
        current = errors.Unwrap(current)
    }

    return builder.String()
}

type runtimeContextKey struct{}

type runtimeContextProbe struct {
    value any
    kind  containercontract.Kind
}

func TestFromRuntime_AScopedProviderReadsTheRuntimesContextAndKind(t *testing.T) {
    for expectedKind, constructor := range map[containercontract.Kind]func(context.Context, containercontract.Scope) containercontract.Runtime{
        containercontract.KindRequest: runtime.NewRequest,
        containercontract.KindRun:     runtime.NewRun,
    } {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        container.MustRegisterScoped[*runtimeContextProbe](serviceContainer, "app.runtime.context.probe", func(resolver containercontract.Resolver) (*runtimeContextProbe, error) {
            return &runtimeContextProbe{value: resolver.Runtime().Context().Value(runtimeContextKey{}), kind: resolver.Runtime().Kind()}, nil
        }, containercontract.RegisterOptions{})

        runtimeInstance := constructor(context.WithValue(t.Context(), runtimeContextKey{}, "the caller's"), serviceContainer.NewScope())

        probe, err := runtime.FromRuntime[*runtimeContextProbe](runtimeInstance, "app.runtime.context.probe")
        if nil != err {
            t.Fatalf("unexpected error: %v", err)
        }

        if "the caller's" != probe.value || expectedKind != probe.kind {
            t.Fatalf("expected the provider to read the runtime's context and kind %s, got %v and %s", expectedKind, probe.value, probe.kind)
        }
    }
}
