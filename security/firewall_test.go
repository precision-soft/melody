package security

import (
    "errors"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

type firewallTestRule struct {
    appliesCallback func(request httpcontract.Request) bool
    checkCallback   func(request httpcontract.Request) error
}

func (instance *firewallTestRule) Applies(request httpcontract.Request) bool {
    return instance.appliesCallback(request)
}

func (instance *firewallTestRule) Check(request httpcontract.Request) error {
    return instance.checkCallback(request)
}

var _ securitycontract.Rule = (*firewallTestRule)(nil)

func TestFirewall_NewFirewall_PanicsOnNilRule(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewFirewall(nil)
}

func TestFirewall_Check_CallsOnlyApplicableRules(t *testing.T) {
    callsA := 0
    callsB := 0

    firewall := NewFirewall(
        &firewallTestRule{
            appliesCallback: func(request httpcontract.Request) bool {
                return true
            },
            checkCallback: func(request httpcontract.Request) error {
                callsA++
                return nil
            },
        },
        &firewallTestRule{
            appliesCallback: func(request httpcontract.Request) bool {
                return false
            },
            checkCallback: func(request httpcontract.Request) error {
                callsB++
                return nil
            },
        },
    )

    err := firewall.Check(newFirewallTestRequest("/admin"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != callsA {
        t.Fatalf("expected rule A to be checked once")
    }
    if 0 != callsB {
        t.Fatalf("expected rule B to not be checked")
    }
}

func TestFirewall_Check_ReturnsFirstError(t *testing.T) {
    expected := errors.New("denied")

    firewall := NewFirewall(
        &firewallTestRule{
            appliesCallback: func(request httpcontract.Request) bool {
                return true
            },
            checkCallback: func(request httpcontract.Request) error {
                return expected
            },
        },
        &firewallTestRule{
            appliesCallback: func(request httpcontract.Request) bool {
                return true
            },
            checkCallback: func(request httpcontract.Request) error {
                t.Fatalf("should not reach second rule after error")
                return nil
            },
        },
    )

    err := firewall.Check(newFirewallTestRequest("/admin"))
    if nil == err {
        t.Fatalf("expected error")
    }
}
