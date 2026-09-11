package security

import "regexp/syntax"

/* Every alternative must assert the beginning of the path before consuming input. */
func accessControlRegexPatternIsAnchored(pattern string) bool {
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
