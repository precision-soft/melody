package security

import (
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewFirewallManager(compiledConfiguration *CompiledConfiguration) *FirewallManager {
    if nil == compiledConfiguration {
        exception.Panic(exception.NewError("compiled security configuration is nil", nil, nil))
    }

    firewalls := make(map[string]*CompiledFirewall)
    for _, firewall := range compiledConfiguration.Firewalls() {
        if nil == firewall {
            continue
        }

        name := firewall.Name()
        if "" == name {
            continue
        }

        firewalls[name] = firewall
    }

    return &FirewallManager{firewalls: firewalls}
}

type FirewallManager struct {
    firewalls map[string]*CompiledFirewall
}

func (instance *FirewallManager) Firewall(name string) (securitycontract.Firewall, error) {
    if "" == name {
        return nil, exception.NewError("firewall name may not be empty", nil, nil)
    }

    firewall, exists := instance.firewalls[name]
    if false == exists || nil == firewall {
        return nil, exception.NewError(
            "firewall not found",
            exceptioncontract.Context{
                "firewallName": name,
            },
            nil,
        )
    }

    return firewall, nil
}

var _ securitycontract.FirewallManager = (*FirewallManager)(nil)
