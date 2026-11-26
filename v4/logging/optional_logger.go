package logging

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* ResolveOptionalLogger answers the logger a component files its failures into: the one handed, the emergency logger when none is; a typed nil is refused as "<component> logger is a typed nil", naming field. */
func ResolveOptionalLogger(logger loggingcontract.Logger, component string, field string) (loggingcontract.Logger, error) {
    if nil == logger {
        return emergencyLoggerProxyInstance, nil
    }

    if true == internal.IsNilInterface(logger) {
        return nil, exception.NewError(
            component+" logger is a typed nil",
            exceptioncontract.Context{
                "field": field,
            },
            nil,
        )
    }

    return logger, nil
}

/* emergencyLoggerProxy reaches the emergency logger on every record, so a component built before CloseEmergencyLogger still writes after it; one instance, so two components defaulted alike hold the same journal. */
type emergencyLoggerProxy struct{}

var emergencyLoggerProxyInstance loggingcontract.Logger = &emergencyLoggerProxy{}

func (instance *emergencyLoggerProxy) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    EmergencyLogger().Log(level, message, context)
}

func (instance *emergencyLoggerProxy) Debug(message string, context loggingcontract.Context) {
    EmergencyLogger().Debug(message, context)
}

func (instance *emergencyLoggerProxy) Info(message string, context loggingcontract.Context) {
    EmergencyLogger().Info(message, context)
}

func (instance *emergencyLoggerProxy) Warning(message string, context loggingcontract.Context) {
    EmergencyLogger().Warning(message, context)
}

func (instance *emergencyLoggerProxy) Error(message string, context loggingcontract.Context) {
    EmergencyLogger().Error(message, context)
}

func (instance *emergencyLoggerProxy) Emergency(message string, context loggingcontract.Context) {
    EmergencyLogger().Emergency(message, context)
}

func (instance *emergencyLoggerProxy) Enabled(level loggingcontract.Level) bool {
    return EmergencyLogger().Enabled(level)
}

func (instance *emergencyLoggerProxy) CauseChainDepth() int {
    return EmergencyLogger().CauseChainDepth()
}
