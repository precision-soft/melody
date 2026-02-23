package security

import (
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
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
    if nil == request {
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
