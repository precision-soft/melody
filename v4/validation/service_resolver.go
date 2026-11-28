package validation

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

func ValidatorMustFromRuntime(runtimeInstance containercontract.Runtime) *Validator {
    return runtime.MustFromRuntime[*Validator](runtimeInstance, ServiceValidator)
}

func ValidatorMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Validator {
    return container.MustFromResolver[*Validator](ctx, resolver, ServiceValidator)
}

/* ValidatorFromRuntime answers nil when the service cannot be resolved, as ValidatorFromResolver does. */
func ValidatorFromRuntime(runtimeInstance containercontract.Runtime) *Validator {
    validatorInstance, err := runtime.FromRuntime[*Validator](runtimeInstance, ServiceValidator)
    if nil == validatorInstance || nil != err {
        return nil
    }

    return validatorInstance
}

func ValidatorFromResolver(ctx context.Context, resolver containercontract.Resolver) *Validator {
    validatorInstance, err := container.FromResolver[*Validator](ctx, resolver, ServiceValidator)
    if nil == validatorInstance || nil != err {
        return nil
    }

    return validatorInstance
}
