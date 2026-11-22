package internal

import (
    "unicode"
)

const (
    ListenerTagRefusalEmpty      = "may not carry an empty tag"
    ListenerTagRefusalWhitespace = "carries a tag with whitespace"
    ListenerTagRefusalTwice      = "carries a tag twice"
)

/* RefusedListenerTag answers the first tag of the list a dispatch could not match exactly: an empty tag, a tag carrying whitespace or a control character, or a tag listed twice; refused is false for a list that passes, nil and an empty list included. */
func RefusedListenerTag(tags []string) (reason string, index int, tag string, refused bool) {
    seenTags := make(map[string]struct{}, len(tags))

    for tagIndex, candidate := range tags {
        if "" == candidate {
            return ListenerTagRefusalEmpty, tagIndex, candidate, true
        }

        for _, character := range candidate {
            if true == unicode.IsSpace(character) || true == unicode.IsControl(character) {
                return ListenerTagRefusalWhitespace, tagIndex, candidate, true
            }
        }

        if _, seen := seenTags[candidate]; true == seen {
            return ListenerTagRefusalTwice, tagIndex, candidate, true
        }

        seenTags[candidate] = struct{}{}
    }

    return "", 0, "", false
}
