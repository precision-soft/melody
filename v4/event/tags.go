package event

import (
    "unicode"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* requireTags refuses a tag list a dispatch could not match exactly: an empty tag, a tag carrying whitespace or a control character, and a tag listed twice. The field names the refusal; nil and an empty list are both untagged. */
func requireTags(field string, eventName string, tags []string) {
    seenTags := make(map[string]struct{}, len(tags))

    for index, tag := range tags {
        if "" == tag {
            exception.Panic(
                exception.NewError(
                    field+" may not carry an empty tag",
                    exceptioncontract.Context{
                        "eventName": eventName,
                        "index":     index,
                    },
                    nil,
                ),
            )
        }

        for _, character := range tag {
            if true == unicode.IsSpace(character) || true == unicode.IsControl(character) {
                exception.Panic(
                    exception.NewError(
                        field+" carries a tag with whitespace",
                        exceptioncontract.Context{
                            "eventName": eventName,
                            "index":     index,
                            "tag":       tag,
                        },
                        nil,
                    ),
                )
            }
        }

        if _, seen := seenTags[tag]; true == seen {
            exception.Panic(
                exception.NewError(
                    field+" carries a tag twice",
                    exceptioncontract.Context{
                        "eventName": eventName,
                        "tag":       tag,
                    },
                    nil,
                ),
            )
        }

        seenTags[tag] = struct{}{}
    }
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
