package config

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* DefaultNamespace is the application namespace when neither MELODY_NAMESPACE nor ApplicationConfig.Namespace names one, so every store name keeps its spelling of the third major. */
const DefaultNamespace = "melody"

const namespaceMaximumLength = 32

/* NamespaceFromRuntime answers the application namespace of the runtime's configuration, DefaultNamespace on any resolution failure, none registered, a closed scope or a wiring error alike, on a typed-nil configuration a foreign scope answers, or when its kernel configuration names none, so a component deriving a default name from the namespace never derives it from an empty one. */
func NamespaceFromRuntime(runtimeInstance containercontract.Runtime) string {
    configuration, configurationErr := ConfigFromRuntime(runtimeInstance)
    if nil != configurationErr || true == internal.IsNilInterface(configuration) {
        return DefaultNamespace
    }

    kernelConfiguration := configuration.Kernel()
    if true == internal.IsNilInterface(kernelConfiguration) || "" == kernelConfiguration.Namespace() {
        return DefaultNamespace
    }

    return kernelConfiguration.Namespace()
}

/* ValidateNamespace is the one rule of an application namespace: 1 to 32 characters of a-z, 0-9, _ and -, beginning and ending with a letter or a digit, so no store separator or glob character can enter a name derived from it. The refusal carries the value; the caller adds the key, field or argument it was read from. */
func ValidateNamespace(namespace string) error {
    if true == isWellFormedNamespace(namespace) {
        return nil
    }

    return exception.NewError(
        "the application namespace must be 1 to 32 characters of a-z, 0-9, _ and -, beginning and ending with a letter or a digit",
        exceptioncontract.Context{"value": namespace},
        nil,
    )
}

func isWellFormedNamespace(namespace string) bool {
    if 0 == len(namespace) || namespaceMaximumLength < len(namespace) {
        return false
    }

    for index := 0; index < len(namespace); index++ {
        character := namespace[index]
        isLetterOrDigit := ('a' <= character && 'z' >= character) || ('0' <= character && '9' >= character)

        if true == isLetterOrDigit {
            continue
        }

        if ('_' != character && '-' != character) || 0 == index || len(namespace)-1 == index {
            return false
        }
    }

    return true
}
