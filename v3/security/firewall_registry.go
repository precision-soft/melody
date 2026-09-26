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
    /* IsNilInterface: a request that cannot be read selects no firewall rather than crashing the walk */
    if true == internal.IsNilInterface(request) {
        return nil, false
    }

    for _, firewall := range instance.compiledConfiguration.Firewalls() {
        /* IsNilInterface: the matcher comes through NewCompiledFirewall unvalidated, and Matches below dereferences it on every request */
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
