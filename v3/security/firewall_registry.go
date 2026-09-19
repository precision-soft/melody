package security

import (
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
)

type FirewallRegistry struct {
    compiledConfiguration *CompiledConfiguration
}

func NewFirewallRegistry(compiledConfiguration *CompiledConfiguration) *FirewallRegistry {
    if nil == compiledConfiguration {
        exception.Panic(exception.NewError("compiled security configuration is nil", nil, nil))
    }

    return &FirewallRegistry{compiledConfiguration: compiledConfiguration}
}

func (instance *FirewallRegistry) Match(request httpcontract.Request) (*CompiledFirewall, bool) {
    /* IsNilInterface and not `nil ==`: a nil pointer of a request type is a non-nil interface a bare check
    carries into the loop below, where the firewall's own matcher dereferences it. The guard exists so a
    request that cannot be read selects nothing rather than crashing the firewall walk. */
    if true == internal.IsNilInterface(request) {
        return nil, false
    }

    for _, firewall := range instance.compiledConfiguration.Firewalls() {
        /* IsNilInterface on the matcher and not `nil ==`: the matcher comes through NewCompiledFirewall, which is public and validates nothing, so a nil pointer of an application's own matcher type arrives as a non-nil interface a bare check reads as live — and Matches below dereferences it, taking down the walk that decides which firewall claims the request, on EVERY request. */
        if nil == firewall || true == internal.IsNilInterface(firewall.Matcher()) {
            continue
        }

        if true == firewall.Matcher().Matches(request) {
            return firewall, true
        }
    }

    return nil, false
}

func (instance *FirewallRegistry) GlobalAccessControl() *AccessControl {
    return instance.compiledConfiguration.GlobalAccessControl()
}
