package translation

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    translationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

const ServiceTranslator = "service.translation.translator"

func TranslatorMustFromContainer(serviceContainer containercontract.Container) translationcontract.Translator {
    return container.MustFromResolver[translationcontract.Translator](serviceContainer, ServiceTranslator)
}

func TranslatorMustFromResolver(resolver containercontract.Resolver) translationcontract.Translator {
    return container.MustFromResolver[translationcontract.Translator](resolver, ServiceTranslator)
}
