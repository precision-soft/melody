package contract

import (
    "io"
)

/* StaticContext supplies command inputs without parsing. Unset flags return zero values rather than declared defaults; callers must supply defaults explicitly. The zero value has no arguments and discards output. Do not modify it concurrently with command execution. */
type StaticContext struct {
    StringValues      map[string]string
    BoolValues        map[string]bool
    IntValues         map[string]int
    StringSliceValues map[string][]string
    /* SetFlagNames distinguishes explicitly supplied values from defaults for IsSet. */
    SetFlagNames   []string
    ArgumentValues []string
    WriterValue    io.Writer
}

var _ Context = (*StaticContext)(nil)

func (instance *StaticContext) String(flagName string) string {
    return instance.StringValues[flagName]
}

func (instance *StaticContext) Bool(flagName string) bool {
    return instance.BoolValues[flagName]
}

func (instance *StaticContext) Int(flagName string) int {
    return instance.IntValues[flagName]
}

func (instance *StaticContext) StringSlice(flagName string) []string {
    return copyStringValues(instance.StringSliceValues[flagName])
}

func (instance *StaticContext) IsSet(flagName string) bool {
    for _, setFlagName := range instance.SetFlagNames {
        if flagName == setFlagName {
            return true
        }
    }

    return false
}

func (instance *StaticContext) Arguments() []string {
    return copyStringValues(instance.ArgumentValues)
}

func (instance *StaticContext) Writer() io.Writer {
    if nil == instance.WriterValue {
        return io.Discard
    }

    return instance.WriterValue
}

func copyStringValues(values []string) []string {
    if nil == values {
        return nil
    }

    copied := make([]string, len(values))
    copy(copied, values)

    return copied
}
