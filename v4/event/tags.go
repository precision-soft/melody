package event

import (
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal/listenertag"
)

/* requireTags refuses a tag list a dispatch could not match exactly or the framework's namespace does not hold: an empty tag, a tag carrying whitespace or a control character, a tag in the melody. namespace the framework does not define, and a tag listed twice. The field names the refusal; nil and an empty list are both untagged. */
func requireTags(field string, eventName string, tags []string) {
    reason, index, tag, refused := listenertag.Refused(tags)
    if false == refused {
        return
    }

    refusalContext := exceptioncontract.Context{"eventName": eventName}
    switch reason {
    case listenertag.RefusalEmpty:
        refusalContext["index"] = index
    case listenertag.RefusalWhitespace:
        refusalContext["index"] = index
        refusalContext["tag"] = tag
    case listenertag.RefusalReserved:
        refusalContext["index"] = index
        refusalContext["tag"] = tag
        refusalContext["frameworkTags"] = listenertag.FrameworkTags()
    default:
        refusalContext["tag"] = tag
    }

    exception.Panic(exception.NewError(field+" "+reason, refusalContext, nil))
}

/* copyTags answers a list of the dispatcher's own, never nil, so a caller's later write reaches nothing and inspection answers [] for none. */
func copyTags(tags []string) []string {
    return append(make([]string, 0, len(tags)), tags...)
}

/* reportedListenerConfig answers the config a report carries, its tags a copy, so a write to a report reaches no dispatch */
func reportedListenerConfig(config eventcontract.ListenerConfig) eventcontract.ListenerConfig {
    config.Tags = copyTags(config.Tags)

    return config
}

/* runsAfterFailure answers whether the listener carries one of the tags its event declared to run after a failure; matching is exact and case-sensitive. */
func runsAfterFailure(listenerTags []string, tagsToRunAfterFailure []string) bool {
    for _, listenerTag := range listenerTags {
        for _, tagToRunAfterFailure := range tagsToRunAfterFailure {
            if listenerTag == tagToRunAfterFailure {
                return true
            }
        }
    }

    return false
}
