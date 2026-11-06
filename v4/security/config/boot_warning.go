package config

import (
    "strings"

    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/security"
)

/* BootWarning is one hazard the compiled declaration admits; the application writes each into the configured journal once, at the http boot */
type BootWarning = internal.BootWarning

const (
    bootWarningShadowedFirewall   = "security.firewallShadowed"
    bootWarningPercentEncodedPath = "security.pathPercentEncoded"
    bootWarningDoorReachUnknown   = "security.firewallDoorReachUnknown"
)

/* compileBootWarnings collects the hazards of a compiled declaration, in declaration order: the firewalls an earlier one shadows, and the declared paths that carry a percent escape */
func compileBootWarnings(compiled *security.CompiledConfiguration) []BootWarning {
    warnings := make([]BootWarning, 0)

    if nil == compiled {
        return warnings
    }

    warnings = append(warnings, shadowedFirewallWarnings(compiled.Firewalls())...)
    warnings = append(warnings, percentEncodedPathWarnings(compiled)...)
    warnings = append(warnings, firewallDoorReachWarnings(compiled.Firewalls())...)

    return warnings
}

/* shadowedFirewallWarnings names every prefix firewall that an earlier one claims whole: the first matching firewall wins, a path prefix is a string prefix and a segment prefix claims its tree, so "/api" declared before "/api-admin" or "/api/admin" takes every request of the later one, whose access control then never runs; a segment prefix "/api" shadows "/api/admin" and not "/api-admin" */
func shadowedFirewallWarnings(firewalls []*security.CompiledFirewall) []BootWarning {
    warnings := make([]BootWarning, 0)

    for laterIndex, laterFirewall := range firewalls {
        laterPrefix, isLaterSegment, isLaterPrefix := firewallPrefix(laterFirewall)
        if false == isLaterPrefix {
            continue
        }

        for _, earlierFirewall := range firewalls[:laterIndex] {
            earlierPrefix, isEarlierSegment, isEarlierPrefix := firewallPrefix(earlierFirewall)
            if false == isEarlierPrefix {
                continue
            }

            if false == prefixClaimsWhole(earlierPrefix, isEarlierSegment, laterPrefix, isLaterSegment) {
                continue
            }

            warnings = append(warnings, BootWarning{
                Name:    bootWarningShadowedFirewall,
                Message: "the firewall is never selected: an earlier firewall's prefix claims every path of its own and the first matching firewall wins, so its requests are resolved and authorized by the earlier one; declare it first, or bound the earlier prefix at a segment boundary with security.NewSegmentPrefixMatcher",
                Context: loggingcontract.Context{
                    "firewall":         laterFirewall.Name(),
                    "entry":            laterPrefix,
                    "shadowedBy":       earlierFirewall.Name(),
                    "shadowedByPrefix": earlierPrefix,
                },
            })

            break
        }
    }

    return warnings
}

func firewallPrefix(firewall *security.CompiledFirewall) (string, bool, bool) {
    switch matcher := firewall.Matcher().(type) {
    case *security.PathPrefixMatcher:
        if nil == matcher {
            return "", false, false
        }

        return matcher.Prefix(), false, true
    case *security.SegmentPrefixMatcher:
        if nil == matcher {
            return "", false, false
        }

        return matcher.Prefix(), true, true
    }

    return "", false, false
}

/* prefixClaimsWhole reports whether the earlier prefix claims every path the later one claims */
func prefixClaimsWhole(earlierPrefix string, isEarlierSegment bool, laterPrefix string, isLaterSegment bool) bool {
    if false == isEarlierSegment {
        if "" == earlierPrefix || "/" == earlierPrefix {
            return true
        }

        if true == isLaterSegment {
            /* "+/" also covers a raw earlier prefix written as the later spelling with its slash */
            return true == strings.HasPrefix(laterPrefix+"/", earlierPrefix)
        }

        return true == strings.HasPrefix(laterPrefix, earlierPrefix)
    }

    if "/" == earlierPrefix {
        return true
    }

    if true == isLaterSegment {
        return laterPrefix == earlierPrefix || true == strings.HasPrefix(laterPrefix, earlierPrefix+"/")
    }

    /* a raw later prefix stays inside the earlier tree only beneath its slash */
    return true == strings.HasPrefix(laterPrefix, earlierPrefix+"/")
}

/* percentEncodedPathWarnings names every declared path that carries a valid percent escape other than %2F: a request path is matched decoded, so such a rule or prefix claims only the resource whose name literally holds the percent sign */
func percentEncodedPathWarnings(compiled *security.CompiledConfiguration) []BootWarning {
    warnings := make([]BootWarning, 0)

    appendWarning := func(firewallName string, declaredPath string) {
        if false == internal.CarriesPercentEscapeOtherThanSeparator(declaredPath) {
            return
        }

        warnings = append(warnings, BootWarning{
            Name:    bootWarningPercentEncodedPath,
            Message: "a declared path carries a percent escape: request paths are matched decoded, with %2F for a separator inside a segment, so the path claims only a resource whose name holds the percent sign literally; write it decoded",
            Context: loggingcontract.Context{
                "firewall": firewallName,
                "entry":    declaredPath,
            },
        })
    }

    appendRules := func(firewallName string, accessControl *security.AccessControl) {
        if nil == accessControl {
            return
        }

        /* a regex rule answers an empty path and carries its declaration as a pattern, so it is never named */
        for _, rule := range accessControl.Rules() {
            appendWarning(firewallName, rule.PathPrefix())
        }
    }

    appendRules("", compiled.GlobalAccessControl())

    for _, firewall := range compiled.Firewalls() {
        if prefix, _, isPrefix := firewallPrefix(firewall); true == isPrefix {
            appendWarning(firewall.Name(), prefix)
        }

        appendRules(firewall.Name(), firewall.AccessControl())
    }

    return warnings
}
