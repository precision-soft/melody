package config

import (
    "regexp"
    "strings"

    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/security"
)

/* BootWarning is one hazard the compiled declaration admits; the application writes each into the configured journal once, at the http boot */
type BootWarning = internal.BootWarning

const (
    bootWarningEmptyFirewallAccessControl = "security.firewallAccessControlEmpty"
    bootWarningShadowedFirewall           = "security.firewallShadowed"
    bootWarningPercentEncodedPath         = "security.pathPercentEncoded"
)

/* percentEscapePattern finds a valid percent escape; the one a declared path may carry is %2F, a separator inside a segment */
var percentEscapePattern = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)

/* compileBootWarnings collects the hazards of a compiled declaration, in declaration order: the firewalls whose access control is the empty one the compile built, the firewalls an earlier one shadows, and the declared paths that carry a percent escape */
func compileBootWarnings(compiled *security.CompiledConfiguration, emptyAccessControlFirewallNames []string) []BootWarning {
    warnings := make([]BootWarning, 0)

    for _, firewallName := range emptyAccessControlFirewallNames {
        warnings = append(warnings, BootWarning{
            Name:    bootWarningEmptyFirewallAccessControl,
            Message: "the firewall declares no access control and inherits none, so the framework compiled an empty one and the firewall authorizes every path it claims for every token; declare WithAccessControl(security.NewAccessControl()) to keep it open on purpose",
            Context: loggingcontract.Context{
                "firewall": firewallName,
            },
        })
    }

    if nil == compiled {
        return warnings
    }

    warnings = append(warnings, shadowedFirewallWarnings(compiled.Firewalls())...)
    warnings = append(warnings, percentEncodedPathWarnings(compiled)...)

    return warnings
}

/* shadowedFirewallWarnings names every path-prefix firewall that an earlier one claims whole: the first matching firewall wins and a path prefix is a string prefix, so "/api" declared before "/api-admin" or "/api/admin" takes every request of the later one, whose access control then never runs */
func shadowedFirewallWarnings(firewalls []*security.CompiledFirewall) []BootWarning {
    warnings := make([]BootWarning, 0)

    for laterIndex, laterFirewall := range firewalls {
        laterMatcher, isLaterPrefix := laterFirewall.Matcher().(*security.PathPrefixMatcher)
        if false == isLaterPrefix || nil == laterMatcher {
            continue
        }

        for _, earlierFirewall := range firewalls[:laterIndex] {
            earlierMatcher, isEarlierPrefix := earlierFirewall.Matcher().(*security.PathPrefixMatcher)
            if false == isEarlierPrefix || nil == earlierMatcher {
                continue
            }

            if false == strings.HasPrefix(laterMatcher.Prefix(), earlierMatcher.Prefix()) {
                continue
            }

            warnings = append(warnings, BootWarning{
                Name:    bootWarningShadowedFirewall,
                Message: "the firewall is never selected: an earlier firewall's path prefix is a string prefix of its own and the first matching firewall wins, so its requests are resolved and authorized by the earlier one; declare it first, or write the earlier prefix up to a segment boundary",
                Context: loggingcontract.Context{
                    "firewall":         laterFirewall.Name(),
                    "entry":            laterMatcher.Prefix(),
                    "shadowedBy":       earlierFirewall.Name(),
                    "shadowedByPrefix": earlierMatcher.Prefix(),
                },
            })

            break
        }
    }

    return warnings
}

/* percentEncodedPathWarnings names every declared path that carries a valid percent escape other than %2F: a request path is matched decoded, so such a rule or prefix claims only the resource whose name literally holds the percent sign */
func percentEncodedPathWarnings(compiled *security.CompiledConfiguration) []BootWarning {
    warnings := make([]BootWarning, 0)

    appendWarning := func(firewallName string, declaredPath string) {
        if false == carriesPercentEscapeOtherThanSeparator(declaredPath) {
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
        if matcher, isPrefix := firewall.Matcher().(*security.PathPrefixMatcher); true == isPrefix && nil != matcher {
            appendWarning(firewall.Name(), matcher.Prefix())
        }

        appendRules(firewall.Name(), firewall.AccessControl())
    }

    return warnings
}

func carriesPercentEscapeOtherThanSeparator(declaredPath string) bool {
    for _, escape := range percentEscapePattern.FindAllString(declaredPath, -1) {
        if false == strings.EqualFold("%2F", escape) {
            return true
        }
    }

    return false
}
