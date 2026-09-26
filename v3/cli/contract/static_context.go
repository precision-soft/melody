package contract

import (
    "io"
    "reflect"
)

/* StaticContext is a Context whose answers are given rather than parsed, for testing a command's body: hand it the values a run would produce and call the command's Run directly. A zero StaticContext answers the zero value of every flag, no arguments and a discarding writer. It does not know which flags a command declares, so an unset name reads as unset, not as the declared default. It is not safe for concurrent modification while a command reads it. */
type StaticContext struct {
    StringValues      map[string]string
    BoolValues        map[string]bool
    IntValues         map[string]int
    StringSliceValues map[string][]string
    /* SetFlagNames are the flags IsSet reports as given on the command line, which is the difference between an explicit value that equals the default and no value at all */
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
    if true == isNilWriter(instance.WriterValue) {
        return io.Discard
    }

    return instance.WriterValue
}

/* isNilWriter reads the shape internal.IsNilInterface reads, spelled here because a contract package imports no concrete melody package: a nil *os.File or *bytes.Buffer handed to WriterValue is a non-nil io.Writer, and it must answer io.Discard like a true nil. */
func isNilWriter(writer io.Writer) bool {
    if nil == writer {
        return true
    }

    reflectedWriter := reflect.ValueOf(writer)

    switch reflectedWriter.Kind() {
    case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
        return true == reflectedWriter.IsNil()
    default:
        return false
    }
}

/* copyStringValues keeps the contract the parsed context keeps: a command that sorts or truncates what it is handed does not rewrite what later readers see */
func copyStringValues(values []string) []string {
    if nil == values {
        return nil
    }

    copied := make([]string, len(values))
    copy(copied, values)

    return copied
}
