package migrate

import (
    "fmt"
    "strings"
)

/* escapeControlCharacters replaces every C0 control character and DEL in the value with its visible escape spelling — the named ones as \n, \r, \t, the rest as \xNN — so text of server origin (an error message off the wire, a reported version string, a failed statement) can be written to the operator's terminal without repainting it or forging lines in a captured log. keepNewline is the multi-line form the query rendering uses, where a real line break is the point and every other control character is still escaped. Mirrors the framework's internal helper, which a separate module cannot import; the json rendering needs none of this, since the encoder escapes on its own. */
func escapeControlCharacters(value string, keepNewline bool) string {
    if false == containsControlCharacter(value, keepNewline) {
        return value
    }

    var builder strings.Builder
    builder.Grow(len(value) + 8)

    for _, currentRune := range value {
        if false == isEscapedControlRune(currentRune, keepNewline) {
            builder.WriteRune(currentRune)

            continue
        }

        builder.WriteString(controlRuneSpelling(currentRune))
    }

    return builder.String()
}

func containsControlCharacter(value string, keepNewline bool) bool {
    for _, currentRune := range value {
        if true == isEscapedControlRune(currentRune, keepNewline) {
            return true
        }
    }

    return false
}

func isEscapedControlRune(currentRune rune, keepNewline bool) bool {
    if '\n' == currentRune {
        return false == keepNewline
    }

    return (0x20 > currentRune && 0 <= currentRune) || 0x7f == currentRune
}

func controlRuneSpelling(currentRune rune) string {
    switch currentRune {
    case '\n':
        return `\n`
    case '\r':
        return `\r`
    case '\t':
        return `\t`
    }

    return fmt.Sprintf(`\x%02x`, currentRune)
}
