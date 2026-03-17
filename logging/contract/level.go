package contract

import "strconv"

type Level string

const (
    /** @internal */
    LevelUnknown Level = "unknown"

    LevelDebug     Level = "debug"
    LevelInfo      Level = "info"
    LevelWarning   Level = "warning"
    LevelError     Level = "error"
    LevelEmergency Level = "emergency"
)

func LevelLabelFromString(s string) LevelLabel {
    return LevelLabel{value: s}
}

func LevelLabelFromInt(i int) LevelLabel {
    return LevelLabel{value: strconv.Itoa(i)}
}

type LevelLabel struct {
    value string
}

func (instance LevelLabel) String() string {
    return instance.value
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

func (instance LevelLabels) LabelFor(level Level) string {
    if label, exists := instance[level]; true == exists && "" != label.value {
        return label.String()
    }

    return string(level)
}
