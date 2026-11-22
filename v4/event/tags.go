package event

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* requireTags refuses a tag list a dispatch could not match exactly: an empty tag, a tag carrying whitespace or a control character, and a tag listed twice. The field names the refusal; nil and an empty list are both untagged. */
func requireTags(field string, eventName string, tags []string) {
    reason, index, tag, refused := internal.RefusedListenerTag(tags)
    if false == refused {
        return
    }

    refusalContext := exceptioncontract.Context{"eventName": eventName}
    switch reason {
    case internal.ListenerTagRefusalEmpty:
        refusalContext["index"] = index
    case internal.ListenerTagRefusalWhitespace:
        refusalContext["index"] = index
        refusalContext["tag"] = tag
    default:
        refusalContext["tag"] = tag
    }

    exception.Panic(exception.NewError(field+" "+reason, refusalContext, nil))
}

/* copyTags answers a list of the dispatcher's own, never nil, so a caller's later write reaches nothing and inspection answers [] for none. */
func copyTags(tags []string) []string {
    return append(make([]string, 0, len(tags)), tags...)
}

/* runsAfterFailure answers whether the listener carries one of the tags its event declared to run after a failure; matching is exact and case-sensitive. */
func runsAfterFailure(listenerTags []string, runAfterFailureTags []string) bool {
    for _, listenerTag := range listenerTags {
        for _, runAfterFailureTag := range runAfterFailureTags {
            if listenerTag == runAfterFailureTag {
                return true
            }
        }
    }

    return false
}
