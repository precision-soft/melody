package security

import (
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
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
        if nil == firewall || nil == firewall.Matcher() {
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
