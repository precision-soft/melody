package accesscontrol

import (
    "regexp/syntax"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* patternIsAnchoredToPathStart reports whether every path the pattern can match starts where the path starts.

The question is asked of the PARSED expression rather than of its text. A textual test — does the pattern begin with "^" — answers for the spelling and not for the language: "^/public|/status" begins with "^" and is read by Go as (^/public)|(/status), whose second branch floats anywhere, so a public rule written that way claims "/admin/status-board". The same textual test refuses patterns that are anchored perfectly well, "(?i)^/public(/|$)" among them, because the flag group comes first.

An alternation is anchored only when EVERY branch is: one floating branch is the whole hole. \A anchors like ^; (?m)^ does not, because it also matches after a newline, and this matcher is reachable from callers that have not refused control characters in the path. */
func patternIsAnchoredToPathStart(pattern string) bool {
    parsedPattern, parseErr := syntax.Parse(pattern, syntax.Perl)
    if nil != parseErr {
        /* an unparseable pattern is refused by the compile below with the parser's own message; it cannot be
           called anchored here, and answering false only routes it to the PUBLIC_ACCESS refusal first */
        return false
    }

    return expressionIsAnchored(parsedPattern.Simplify())
}

func expressionIsAnchored(expression *syntax.Regexp) bool {
    if nil == expression {
        return false
    }

    switch expression.Op {
    case syntax.OpBeginText:
        return true

    case syntax.OpConcat:
        /* a concatenation is anchored by its first element that can consume or assert anything; an empty
           match in front of the anchor (a zero-width group, an empty capture) does not move the start */
        for _, subExpression := range expression.Sub {
            if true == expressionMatchesEmptyOnly(subExpression) {
                continue
            }

            return expressionIsAnchored(subExpression)
        }

        return false

    case syntax.OpAlternate:
        /* every branch must be anchored: one floating branch is the whole hole */
        for _, subExpression := range expression.Sub {
            if false == expressionIsAnchored(subExpression) {
                return false
            }
        }

        return 0 < len(expression.Sub)

    case syntax.OpCapture:
        if 1 != len(expression.Sub) {
            return false
        }

        return expressionIsAnchored(expression.Sub[0])

    case syntax.OpPlus:
        /* x+ starts where x starts, so it is anchored when x is */
        if 1 != len(expression.Sub) {
            return false
        }

        return expressionIsAnchored(expression.Sub[0])
    }

    return false
}

/* expressionMatchesEmptyOnly reports whether the expression consumes nothing and asserts nothing about position, so it cannot move where the match begins. */
func expressionMatchesEmptyOnly(expression *syntax.Regexp) bool {
    if nil == expression {
        return false
    }

    switch expression.Op {
    case syntax.OpEmptyMatch, syntax.OpNoMatch:
        return true

    case syntax.OpCapture:
        if 1 != len(expression.Sub) {
            return false
        }

        return expressionMatchesEmptyOnly(expression.Sub[0])
    }

    return false
}

/* refuseUnanchoredPublicAccess refuses PUBLIC_ACCESS on a pattern that can match inside a path rather than at its start. Such a rule opens every path the pattern reaches as a substring — "/status" grants "/admin/status-board", a protected route reached through a public rule — and among regex rules the first registered that matches wins, so a public substring rule shadows a stricter regex declared after it. It is the same over-open NewRawPrefixRule refuses PUBLIC_ACCESS for. A start-anchored pattern cannot reach into the middle of a protected path and stays allowed. */
func refuseUnanchoredPublicAccess(pattern string, attributes []string) {
    for _, attribute := range attributes {
        if securitycontract.AttributePublicAccess != strings.TrimSpace(attribute) {
            continue
        }

        if true == patternIsAnchoredToPathStart(pattern) {
            continue
        }

        exception.Panic(
            exception.NewError(
                "access control PUBLIC_ACCESS may not be declared on a regex rule that can match inside a path; anchor every branch of the pattern to the path start with ^ (for example ^/public(/|$))",
                map[string]any{"pattern": pattern},
                nil,
            ),
        )
    }
}
