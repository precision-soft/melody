package accesscontrol

import (
    "regexp/syntax"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* patternIsAnchoredToPathStart reports whether every path the pattern can match starts where the path starts. It reads the parsed expression, not the text: "^/public|/status" begins with "^" while its second branch floats, and "(?i)^/public(/|$)" is anchored behind its flag group. An alternation is anchored only when every branch is; \A anchors like ^, (?m)^ does not. */
func patternIsAnchoredToPathStart(pattern string) bool {
    parsedPattern, parseErr := syntax.Parse(pattern, syntax.Perl)
    if nil != parseErr {
        /* an unparseable pattern is refused by the compile below; answering false only routes it to the PUBLIC_ACCESS refusal first */
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
        /* a concatenation is anchored by its first element that consumes or asserts anything; a zero-width element in front of the anchor does not move the start */
        for _, subExpression := range expression.Sub {
            if true == expressionMatchesEmptyOnly(subExpression) {
                continue
            }

            return expressionIsAnchored(subExpression)
        }

        return false

    case syntax.OpAlternate:
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

/* refuseUnanchoredPublicAccess refuses PUBLIC_ACCESS on a pattern that can match inside a path: such a rule opens every path it reaches as a substring, "/status" granting "/admin/status-board", and since the first matching regex rule wins it would shadow a stricter one declared after it. */
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
