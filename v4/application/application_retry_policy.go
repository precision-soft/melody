package application

import (
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* retiredRetryPolicyServiceName is the third major's container door for the consume command's retry policy, read by nothing in the fourth */
const retiredRetryPolicyServiceName = "service.messagebus.retry_policy"

/* a policy still registered under the third major's name changes nothing, so the boot names it rather than let a deployment believe it is read */
func (instance *Application) collectIgnoredRetryPolicyService() {
    if nil == instance.kernel || true == internal.IsNilInterface(instance.kernel.ServiceContainer()) {
        return
    }

    if false == instance.kernel.ServiceContainer().Has(retiredRetryPolicyServiceName) {
        return
    }

    instance.bootWarnings = append(instance.bootWarnings, internal.BootWarning{
        Name:    internal.BootWarningNameRetryPolicyServiceIgnored,
        Message: "a service is registered under the retry policy's third-major name, which nothing reads: the framework's consume command takes ApplicationConfig.MessageBus.Retry",
        Context: loggingcontract.Context{
            "serviceName": retiredRetryPolicyServiceName,
        },
    })
}
