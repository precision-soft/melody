package application

import (
    "slices"
    "sort"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type frameworkEventDeclaration struct {
    eventName string
    config    eventcontract.EventConfig
}

/* frameworkEvents are the framework's own events with the tags its listeners carry, in the order they are declared and named in a refusal */
func frameworkEvents() []frameworkEventDeclaration {
    return []frameworkEventDeclaration{
        {eventName: kernelcontract.EventKernelRequest},
        {eventName: kernelcontract.EventKernelController},
        {eventName: kernelcontract.EventKernelResponse, config: eventcontract.EventConfig{TagsToRunAfterFailure: []string{kernelcontract.ListenerTagKernelCors}}},
        {eventName: kernelcontract.EventKernelTerminate, config: eventcontract.EventConfig{TagsToRunAfterFailure: []string{kernelcontract.ListenerTagKernelAccessLog}}},
        {eventName: kernelcontract.EventKernelException},
        {eventName: securitycontract.EventSecurityAuthorizationGranted},
        {eventName: securitycontract.EventSecurityAuthorizationDenied},
        {eventName: securitycontract.EventSecurityLoginSuccess},
        {eventName: securitycontract.EventSecurityLoginFailure},
        {eventName: securitycontract.EventSecurityLogoutSuccess},
        {eventName: securitycontract.EventSecurityLogoutFailure},
        {eventName: httpcontract.EventHttpRequestProfile},
    }
}

/* declareFrameworkEvents declares the framework's twelve events on the dispatcher, each with the framework's tags followed by the application's, under the dispatcher's own rules; a key that is none of the twelve is refused before anything is declared, and a tag the dispatcher refuses is answered naming the application's field */
func declareFrameworkEvents(eventDispatcher eventcontract.EventDispatcher, events map[string]eventcontract.EventConfig) error {
    declarations := frameworkEvents()

    frameworkEventNames := make([]string, 0, len(declarations))
    for _, declaration := range declarations {
        frameworkEventNames = append(frameworkEventNames, declaration.eventName)
    }

    eventNames := make([]string, 0, len(events))
    for eventName := range events {
        eventNames = append(eventNames, eventName)
    }
    sort.Strings(eventNames)

    for _, eventName := range eventNames {
        if false == slices.Contains(frameworkEventNames, eventName) {
            return exception.NewError(
                "ApplicationConfig.Events names an event the framework does not own; an application event is declared through EventDispatcher.DeclareEvent",
                exceptioncontract.Context{
                    "eventName":       eventName,
                    "frameworkEvents": frameworkEventNames,
                },
                nil,
            )
        }
    }

    for _, declaration := range declarations {
        frameworkTags := declaration.config.TagsToRunAfterFailure
        applicationTags := events[declaration.eventName].TagsToRunAfterFailure

        mergedTags := append(append(make([]string, 0, len(frameworkTags)+len(applicationTags)), frameworkTags...), applicationTags...)

        declareErr := declareFrameworkEvent(eventDispatcher, declaration.eventName, eventcontract.EventConfig{TagsToRunAfterFailure: mergedTags})
        if nil != declareErr {
            return exception.NewError(
                "ApplicationConfig.Events is refused",
                exceptioncontract.Context{
                    "eventName":     declaration.eventName,
                    "field":         `ApplicationConfig.Events["` + declaration.eventName + `"].TagsToRunAfterFailure`,
                    "frameworkTags": append(make([]string, 0, len(frameworkTags)), frameworkTags...),
                },
                declareErr,
            )
        }
    }

    return nil
}

/* declareFrameworkEvent answers the dispatcher's refusal of one declaration as an error; any other panic, an exit included, passes */
func declareFrameworkEvent(eventDispatcher eventcontract.EventDispatcher, eventName string, config eventcontract.EventConfig) (refusal error) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        refusalErr, isRefusal := recovered.(*exception.Error)
        if false == isRefusal || nil == refusalErr {
            panic(recovered)
        }

        refusal = refusalErr
    }()

    eventDispatcher.DeclareEvent(eventName, config)

    return nil
}
