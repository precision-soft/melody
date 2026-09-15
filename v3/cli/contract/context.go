package contract

import (
    "io"
)

/* Context supplies a command’s parsed flags, positional arguments and output writer. */
type Context interface {
    /* String returns the string flag value or its declared default when absent. */
    String(flagName string) string

    /* Bool returns the boolean flag value or its declared default when absent. */
    Bool(flagName string) bool

    /* Int returns the integer flag value or its declared default when absent. */
    Int(flagName string) int

    /* StringSlice returns repeatable flag values in command-line order. */
    StringSlice(flagName string) []string

    /* IsSet distinguishes an explicitly supplied flag from its default value. */
    IsSet(flagName string) bool

    /* Arguments returns an independent copy of the positional arguments. */
    Arguments() []string

    /* Writer is never nil; a context without an output stream uses io.Discard. */
    Writer() io.Writer
}
