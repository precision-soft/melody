package contract

import (
    "io"
)

/* Context is what a command's Run receives: the flags the command declared, already parsed, and the positional arguments left over. It is melody's own contract, so the flag parsing engine behind it is an implementation detail of the cli package and no consumer of this interface ever names it — before this contract existed the type handed to a command was the engine's own command struct, which put every field that struct has, mutable, into melody's public surface and made the engine's API part of melody's compatibility promise. */
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

    /* Arguments answers the positional arguments left after the flags, as a copy: a command is free to sort or truncate what it is handed without reaching into what the engine still holds. */
    Arguments() []string

    /* Writer answers the stream the command's own output belongs on. It is never nil — a context built without one answers io.Discard, so a command does not have to guard the very first line it writes. */
    Writer() io.Writer
}
