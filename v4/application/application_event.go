package application

import (
    "sort"
    "strings"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* frameworkListenerTagPrefix is the namespace of the tags the framework's own listeners carry */
const frameworkListenerTagPrefix = "melody."

type frameworkEventDeclaration struct {
    eventName string
    config    eventcontract.EventConfig
}

/* frameworkEvents are the framework's own events with the tags its listeners carry, in the order they are declared and named in a refusal */
func frameworkEvents() []frameworkEventDeclaration {
    return []frameworkEventDeclaration{
        {eventName: kernelcontract.EventKernelRequest},
        {eventName: kernelcontract.EventKernelController},
        {eventName: kernelcontract.EventKernelResponse, config: eventcontract.EventConfig{RunAfterFailureTags: []string{kernelcontract.ListenerTagCors}}},
        {eventName: kernelcontract.EventKernelTerminate, config: eventcontract.EventConfig{RunAfterFailureTags: []string{kernelcontract.ListenerTagAccessLog}}},
        {eventName: kernelcontract.EventKernelException},
        {eventName: securitycontract.EventSecurityAuthorizationGranted},
        {eventName: securitycontract.EventSecurityAuthorizationDenied},
        {eventName: securitycontract.EventSecurityLoginSuccess},
        {eventName: securitycontract.EventSecurityLoginFailure},
        {eventName: securitycontract.EventSecurityLogoutSuccess},
        {eventName: securitycontract.EventSecurityLogoutFailure},
        {eventName: http.EventHttpRequestProfile},
    }
}

/* resolveFrameworkEventConfigs answers the twelve declarations, each with the framework's tags followed by the application's; the first refusal in sorted key order is answered */
func resolveFrameworkEventConfigs(events map[string]eventcontract.EventConfig) ([]frameworkEventDeclaration, error) {
    declarations := frameworkEvents()

    indexByName := make(map[string]int, len(declarations))
    frameworkEventNames := make([]string, 0, len(declarations))
    for index, declaration := range declarations {
        indexByName[declaration.eventName] = index
        frameworkEventNames = append(frameworkEventNames, declaration.eventName)
    }

    eventNames := make([]string, 0, len(events))
    for eventName := range events {
        eventNames = append(eventNames, eventName)
    }
    sort.Strings(eventNames)

    for _, eventName := range eventNames {
        index, owned := indexByName[eventName]
        if false == owned {
            return nil, exception.NewError(
                "ApplicationConfig.Events names an event the framework does not own; an application event is declared through EventDispatcher.DeclareEvent",
                exceptioncontract.Context{
                    "eventName":       eventName,
                    "frameworkEvents": frameworkEventNames,
                },
                nil,
            )
        }

        field := `ApplicationConfig.Events["` + eventName + `"].RunAfterFailureTags`
        applicationTags := events[eventName].RunAfterFailureTags

        if reason, tagIndex, tag, refused := internal.RefusedListenerTag(applicationTags); true == refused {
            return nil, exception.NewError(
                field+" "+reason,
                exceptioncontract.Context{
                    "eventName": eventName,
                    "index":     tagIndex,
                    "tag":       tag,
                },
                nil,
            )
        }

        for tagIndex, tag := range applicationTags {
            if true == strings.HasPrefix(tag, frameworkListenerTagPrefix) {
                return nil, exception.NewError(
                    "ApplicationConfig.Events carries a tag in the melody. namespace, which the framework declares itself",
                    exceptioncontract.Context{
                        "field":     field,
                        "eventName": eventName,
                        "index":     tagIndex,
                        "tag":       tag,
                    },
                    nil,
                )
            }
        }

        mergedTags := append(append(make([]string, 0, len(declarations[index].config.RunAfterFailureTags)+len(applicationTags)), declarations[index].config.RunAfterFailureTags...), applicationTags...)
        declarations[index].config = eventcontract.EventConfig{RunAfterFailureTags: mergedTags}
    }

    return declarations, nil
}
