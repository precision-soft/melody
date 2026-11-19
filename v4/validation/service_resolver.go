package validation

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func ValidatorMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Validator {
    return container.MustFromResolver[*Validator](ctx, resolver, ServiceValidator)
}

func ValidatorFromResolver(ctx context.Context, resolver containercontract.Resolver) *Validator {
    validatorInstance, err := container.FromResolver[*Validator](ctx, resolver, ServiceValidator)
    if nil == validatorInstance || nil != err {
        return nil
    }

    return validatorInstance
}
