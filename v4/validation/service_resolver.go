package validation

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func ValidatorMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) *Validator {
    return container.MustFromResolver[*Validator](ctx, serviceContainer, ServiceValidator)
}

func ValidatorFromContainer(ctx context.Context, serviceContainer containercontract.Container) *Validator {
    validatorInstance, err := container.FromResolver[*Validator](ctx, serviceContainer, ServiceValidator)
    if nil == validatorInstance || nil != err {
        return nil
    }

    return validatorInstance
}
