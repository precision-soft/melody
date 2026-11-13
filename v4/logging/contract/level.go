package contract

import (
    "encoding/json"
    "fmt"
    "strconv"
)

type Level string

const (
    LevelUnknown Level = "unknown"

    LevelDebug     Level = "debug"
    LevelInfo      Level = "info"
    LevelWarning   Level = "warning"
    LevelError     Level = "error"
    LevelEmergency Level = "emergency"
)

func LevelLabelFromString(label string) LevelLabel {
    return LevelLabel{value: label}
}

func LevelLabelFromInt(label int) LevelLabel {
    return LevelLabel{value: label}
}

type LevelLabel struct {
    value any
}

func (instance LevelLabel) String() string {
    switch typedValue := instance.value.(type) {
    case int:
        return strconv.Itoa(typedValue)
    case string:
        return typedValue
    default:
        return fmt.Sprintf("%v", typedValue)
    }
}

func (instance LevelLabel) MarshalJSON() ([]byte, error) {
    return json.Marshal(instance.value)
}

func DefaultLevelLabels() LevelLabels {
    return LevelLabels{
        LevelDebug:     LevelLabelFromString("debug"),
        LevelInfo:      LevelLabelFromString("info"),
        LevelWarning:   LevelLabelFromString("warning"),
        LevelError:     LevelLabelFromString("error"),
        LevelEmergency: LevelLabelFromString("emergency"),
    }
}

type LevelLabels map[Level]LevelLabel

func (instance LevelLabels) LabelFor(level Level) LevelLabel {
    label, exists := instance[level]
    if false == exists {
        return LevelLabelFromString(string(level))
    }

    switch label.value.(type) {
    case int:
        return label
    case string:
        return label
    }

    return LevelLabelFromString(string(level))
}
