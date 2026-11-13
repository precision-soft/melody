package internal

import (
    "regexp"
    "strings"
)

/* percentEscapePattern finds a valid percent escape; the one a declared path may carry is %2F, a separator inside a segment */
var percentEscapePattern = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)

/* CarriesPercentEscapeOtherThanSeparator reports a declared path holding a valid percent escape other than %2F: a request path is matched decoded, so such a rule, prefix or exclusion claims only a resource whose name literally holds the percent sign */
func CarriesPercentEscapeOtherThanSeparator(declaredPath string) bool {
    for _, escape := range percentEscapePattern.FindAllString(declaredPath, -1) {
        if false == strings.EqualFold("%2F", escape) {
            return true
        }
    }

    return false
}
