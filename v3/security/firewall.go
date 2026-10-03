package security

import (
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewFirewall(rules ...securitycontract.Rule) *Firewall {
    for index, rule := range rules {
        if true == internal.IsNilInterface(rule) {
            exception.Panic(
                exception.NewError(
                    "security firewall rule is nil",
                    exceptioncontract.Context{
                        "index": index,
                    },
                    nil,
                ),
            )
        }
    }

    return &Firewall{
        rules: append([]securitycontract.Rule{}, rules...),
    }
}

type Firewall struct {
    rules []securitycontract.Rule
}

func (instance *Firewall) Check(request httpcontract.Request) error {
    for _, rule := range instance.rules {
        if false == rule.Applies(request) {
            continue
        }

        err := rule.Check(request)
        if nil != err {
            return err
        }
    }

    return nil
}
