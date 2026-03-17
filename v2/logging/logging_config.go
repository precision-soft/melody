package logging

import (
    "fmt"

    "github.com/precision-soft/melody/v2/exception"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

func NewLoggingConfiguration(labels loggingcontract.LevelLabels) loggingcontract.LoggingConfiguration {
    return &loggingConfiguration{levelLabels: labels}
}

type loggingConfiguration struct {
    levelLabels loggingcontract.LevelLabels
}

func (instance *loggingConfiguration) LevelLabels() loggingcontract.LevelLabels {
    return instance.levelLabels
}

func LoggingConfigurationFromModules(moduleConfigurations map[string]any) loggingcontract.LoggingConfiguration {
    if nil == moduleConfigurations {
        return &loggingConfiguration{levelLabels: loggingcontract.DefaultLevelLabels()}
    }

    raw, exists := moduleConfigurations[loggingcontract.LoggingConfigurationName]
    if false == exists {
        return &loggingConfiguration{levelLabels: loggingcontract.DefaultLevelLabels()}
    }

    if nil == raw {
        exception.Panic(
            exception.NewEmergency(
                "invalid logging configuration",
                map[string]any{
                    "configurationName": loggingcontract.LoggingConfigurationName,
                    "expectedType":      "loggingcontract.LoggingConfiguration",
                    "actualType":        "<nil>",
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
