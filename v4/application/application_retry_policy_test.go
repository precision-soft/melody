package application

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/messagebus"
)

func retryPolicyServiceWarningsOf(applicationInstance *Application) []internal.BootWarning {
    warnings := make([]internal.BootWarning, 0)
    for _, bootWarning := range applicationInstance.bootWarnings {
        if internal.BootWarningNameRetryPolicyServiceIgnored == bootWarning.Name {
            warnings = append(warnings, bootWarning)
        }
    }

    return warnings
}

func TestBoot_ARetryPolicyRegisteredUnderTheThirdMajorsNameIsNamedInABootWarning(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.RegisterService("service.messagebus.retry_policy", func(resolver containercontract.Resolver) (messagebus.RetryPolicy, error) {
        return messagebus.RetryPolicy{}, nil
    }, containercontract.RegisterOptions{})

    applicationInstance.Boot()

    warnings := retryPolicyServiceWarningsOf(applicationInstance)
    if 1 != len(warnings) || "service.messagebus.retry_policy" != warnings[0].Context["serviceName"] {
        t.Fatalf("expected one warning naming the service, got %+v", warnings)
    }

    clean := newEnvironmentKeyBootApplication(t, nil)
    clean.Boot()

    if warnings := retryPolicyServiceWarningsOf(clean); 0 != len(warnings) {
        t.Fatalf("expected no warning without the service, got %+v", warnings)
    }
}
