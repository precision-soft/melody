package logging

import (
    "fmt"
    "log"
    "sort"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* DefaultLoggerConfig configures the logger writing through the standard library's log package; the zero value is the framework's. */
type DefaultLoggerConfig struct {
    /* Labels names the levels in the record; a level it leaves out, every level when nil, is named as the default labels name it. */
    Labels loggingcontract.LevelLabels

    /* CauseChainDepth is how many links of an *exception.Error's cause chain LogError writes; 8 when left out, at least 1, unlimited refused since a chain can be cyclic. The chain of any other error is the one exception.LogContext builds. */
    CauseChainDepth bound.Count
}

var defaultLoggerCauseChainDepthRule = causeChainDepthRule("DefaultLoggerConfig.CauseChainDepth")

/* NewDefaultLogger writes one text line per record through the standard library's log package, refusing a config field outside its rule by name. */
func NewDefaultLogger(config DefaultLoggerConfig) loggingcontract.Logger {
    causeChainDepth, causeChainDepthErr := bound.Resolve(config.CauseChainDepth, defaultLoggerCauseChainDepthRule)
    if nil != causeChainDepthErr {
        exception.Panic(exception.FromError(causeChainDepthErr))
    }

    /* the labels are copied: the map is read lock-free on every Log call, so a caller mutating the map it still holds would be a fatal concurrent map access */
    return &defaultLogger{
        levelLabels:     copyLevelLabels(config.Labels),
        causeChainDepth: causeChainDepth.Value,
    }
}

type defaultLogger struct {
    levelLabels     loggingcontract.LevelLabels
    causeChainDepth int
}

func (instance *defaultLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if nil == context {
        context = loggingcontract.Context{}
    }

    /* one record stays one line: the message and context embed request-derived text, and an unescaped line break would start a forged record at whatever level the payload names */
    log.Printf(
        "[%s] %s %s",
        instance.levelLabels.LabelFor(level),
        internal.EscapeControlCharacters(message),
        internal.EscapeControlCharacters(instance.formatContext(context)),
    )
}

func (instance *defaultLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *defaultLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *defaultLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *defaultLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *defaultLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *defaultLogger) formatContext(context loggingcontract.Context) string {
    if 0 == len(context) {
        return ""
    }

    keys := make([]string, 0, len(context))
    for key := range context {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    pairs := make([]string, 0, len(context))
    for _, key := range keys {
        pairs = append(pairs, key+"="+renderTextValue(context[key]))
    }

    return fmt.Sprintf("{%s}", instance.joinPairs(pairs))
}

func (instance *defaultLogger) joinPairs(values []string) string {
    result := ""
    for index, value := range values {
        if 0 < index {
            result += " "
        }
        result += value
    }
    return result
}

func (instance *defaultLogger) CauseChainDepth() int {
    return instance.causeChainDepth
}

var _ loggingcontract.Logger = (*defaultLogger)(nil)
var _ loggingcontract.CauseChainDepthReporter = (*defaultLogger)(nil)
