package security

import (
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

func NewFirewall(rules ...securitycontract.Rule) *Firewall {
    for index, rule := range rules {
        if nil == rule {
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
        rules: rules,
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
