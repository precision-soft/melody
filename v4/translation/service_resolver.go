package translation

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

const ServiceTranslator = "service.translation.translator"

func TranslatorMustFromRuntime(runtimeInstance containercontract.Runtime) translationcontract.Translator {
    return runtime.MustFromRuntime[translationcontract.Translator](runtimeInstance, ServiceTranslator)
}

func TranslatorMustFromResolver(ctx context.Context, resolver containercontract.Resolver) translationcontract.Translator {
    return container.MustFromResolver[translationcontract.Translator](ctx, resolver, ServiceTranslator)
}
