package translation

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

const ServiceTranslator = "service.translation.translator"

func TranslatorMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) translationcontract.Translator {
    return container.MustFromResolver[translationcontract.Translator](ctx, serviceContainer, ServiceTranslator)
}

func TranslatorMustFromResolver(ctx context.Context, resolver containercontract.Resolver) translationcontract.Translator {
    return container.MustFromResolver[translationcontract.Translator](ctx, resolver, ServiceTranslator)
}
