package security

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
)

func TestNewFirewallManager_NilConfigurationPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewFirewallManager(nil)
        },
        "compiled security configuration is nil",
    )
}

func TestFirewallManager_FirewallAnswersTheNamedFirewall(t *testing.T) {
    firewallMain := newTestCompiledFirewallWithRoleHierarchy("main", nil)
    firewallApi := newTestCompiledFirewallWithRoleHierarchy("api", nil)

    manager := NewFirewallManager(
        NewCompiledConfiguration([]*CompiledFirewall{firewallMain, firewallApi}, nil),
    )

    found, err := manager.Firewall("api")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if firewallApi != found {
        t.Fatalf("expected the firewall named api to be answered")
    }
}

func TestFirewallManager_FirewallRefusesAnEmptyName(t *testing.T) {
    manager := NewFirewallManager(
        NewCompiledConfiguration(
            []*CompiledFirewall{newTestCompiledFirewallWithRoleHierarchy("main", nil)},
            nil,
        ),
    )

    found, err := manager.Firewall("")
    if nil == err {
        t.Fatalf("expected an empty name to be refused")
    }

    if nil != found {
        t.Fatalf("expected no firewall alongside the refusal")
    }

    if false == strings.Contains(err.Error(), "firewall name may not be empty") {
        t.Fatalf("expected the refusal to name the empty name, got %q", err.Error())
    }
}

func TestFirewallManager_FirewallRefusesAnUnknownName(t *testing.T) {
    manager := NewFirewallManager(
        NewCompiledConfiguration(
            []*CompiledFirewall{newTestCompiledFirewallWithRoleHierarchy("main", nil)},
            nil,
        ),
    )

    found, err := manager.Firewall("absent")
    if nil == err {
        t.Fatalf("expected an unknown name to be refused")
    }

    if nil != found {
        t.Fatalf("expected no firewall alongside the refusal")
    }

    contextValue := exception.LogContext(err)
    if "absent" != contextValue["firewallName"] {
        t.Fatalf("expected the refused name in the error context, got %v", contextValue["firewallName"])
    }
}

func TestNewFirewallManager_SkipsNilFirewalls(t *testing.T) {
    firewallMain := newTestCompiledFirewallWithRoleHierarchy("main", nil)

    manager := NewFirewallManager(
        NewCompiledConfiguration([]*CompiledFirewall{nil, firewallMain}, nil),
    )

    found, err := manager.Firewall("main")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if firewallMain != found {
        t.Fatalf("expected the named firewall to survive a nil sibling")
    }
}

func TestNewFirewallManager_SkipsUnnamedFirewalls(t *testing.T) {
    manager := NewFirewallManager(
        NewCompiledConfiguration(
            []*CompiledFirewall{newTestCompiledFirewallWithRoleHierarchy("", nil)},
            nil,
        ),
    )

    _, err := manager.Firewall("")
    if nil == err {
        t.Fatalf("expected the unnamed firewall to be unreachable")
    }
}

func TestNewFirewallManager_LastFirewallWinsOnADuplicateName(t *testing.T) {
    firewallFirst := newTestCompiledFirewallWithRoleHierarchy("main", nil)
    firewallSecond := newTestCompiledFirewallWithRoleHierarchy("main", NewRoleHierarchy(nil))

    manager := NewFirewallManager(
        NewCompiledConfiguration([]*CompiledFirewall{firewallFirst, firewallSecond}, nil),
    )

    found, err := manager.Firewall("main")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if firewallSecond != found {
        t.Fatalf("expected the last firewall registered under the name to win")
    }
}
