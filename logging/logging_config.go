package logging

import (
    "fmt"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* NewLoggingConfiguration copies the labels on the way in, and LevelLabels hands a copy back out: the map is read lock-free on every Log call of the logger built from it, so a reference kept by the caller turns a later write into a fatal concurrent map access no recover reaches. */
func NewLoggingConfiguration(labels loggingcontract.LevelLabels) loggingcontract.LoggingConfiguration {
    return &loggingConfiguration{levelLabels: copyLevelLabels(labels)}
}

type loggingConfiguration struct {
    levelLabels loggingcontract.LevelLabels
}

func (instance *loggingConfiguration) LevelLabels() loggingcontract.LevelLabels {
    return copyLevelLabels(instance.levelLabels)
}

func copyLevelLabels(labels loggingcontract.LevelLabels) loggingcontract.LevelLabels {
    copied := make(loggingcontract.LevelLabels, len(labels))
    for level, label := range labels {
        copied[level] = label
    }

    return copied
}

func LoggingConfigurationFromModules(moduleConfigurations map[string]any) loggingcontract.LoggingConfiguration {
    if nil == moduleConfigurations {
        return &loggingConfiguration{levelLabels: loggingcontract.DefaultLevelLabels()}
    }

    raw, exists := moduleConfigurations[loggingcontract.LoggingConfigurationName]
    if false == exists {
        return &loggingConfiguration{levelLabels: loggingcontract.DefaultLevelLabels()}
    }

    /* the typed nil is refused above the assertion, which would otherwise accept it: the boot then dies inside the logger provider, naming service.logger instead of the configuration that was registered wrong */
    if nil == raw || true == internal.IsNilInterface(raw) {
        actualType := "<nil>"
        if nil != raw {
            actualType = fmt.Sprintf("%T", raw)
        }

        exception.Panic(
            exception.NewEmergency(
                "invalid logging configuration",
                map[string]any{
                    "configurationName": loggingcontract.LoggingConfigurationName,
                    "expectedType":      "loggingcontract.LoggingConfiguration",
                    "actualType":        actualType,
                },
                nil,
            ),
        )

        return nil
    }

    loggingConfig, ok := raw.(loggingcontract.LoggingConfiguration)
    if false == ok {
        exception.Panic(
            exception.NewEmergency(
                "invalid logging configuration",
                map[string]any{
                    "configurationName": loggingcontract.LoggingConfigurationName,
                    "expectedType":      "loggingcontract.LoggingConfiguration",
                    "actualType":        fmt.Sprintf("%T", raw),
                },
                nil,
            ),
        )

        return nil
    }

    return loggingConfig
}

var _ loggingcontract.LoggingConfiguration = (*loggingConfiguration)(nil)
