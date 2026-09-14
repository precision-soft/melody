package contract

import (
    "io"
)

/* Context supplies a command’s parsed flags, positional arguments and output writer. */
type Context interface {
    /* String answers the value of a string flag, or its declared default when the flag was not given. */
    String(flagName string) string

    /* Bool answers the value of a bool flag, or its declared default when the flag was not given. */
    Bool(flagName string) bool

    /* Int answers the value of an int flag, or its declared default when the flag was not given. */
    Int(flagName string) int

    /* StringSlice answers the values of a repeatable string flag, in the order they were given. */
    StringSlice(flagName string) []string

    /* IsSet reports whether the flag was given on the command line at all, which is the difference between an explicit value that equals the default and no value. */
    IsSet(flagName string) bool

    /* Arguments returns an independent copy of the positional arguments. */
    Arguments() []string

    /* Writer is never nil; a context without an output stream uses io.Discard. */
    Writer() io.Writer
}
