package accesscontrol

import (
    "regexp/syntax"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func patternIsAnchoredToPathStart(pattern string) bool {
    parsedPattern, parseErr := syntax.Parse(pattern, syntax.Perl)
    if nil != parseErr {

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
