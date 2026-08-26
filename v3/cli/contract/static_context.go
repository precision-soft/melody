package contract

import (
    "io"
)

/* StaticContext is a Context whose answers are given rather than parsed. It exists because a command's body is worth testing without a command line: before melody owned this contract a caller could build the engine's command struct itself and drive argv through it, and taking that away without offering a door would have made a command harder to test than it was. Hand it the values a run would have produced and call the command's Run directly.

A zero StaticContext answers the zero value of every flag, no arguments, and a writer that discards. It is a value holder, not a parser: it does not know which flags a command declares, so a name nothing set reads as unset rather than as the flag's declared default — pass the default when the case under test depends on it. It is not safe for concurrent modification while a command reads it. */
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
    if nil == instance.WriterValue {
        return io.Discard
    }

    return instance.WriterValue
}

/* copyStringValues keeps the contract the parsed context keeps: what a command is handed is its own, so a command that sorts or truncates it does not rewrite the values every later reader sees */
func copyStringValues(values []string) []string {
    if nil == values {
        return nil
    }

    copied := make([]string, len(values))
    copy(copied, values)

    return copied
}
