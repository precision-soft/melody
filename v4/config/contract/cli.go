package contract

type CliConfiguration interface {
    Name() string

    Description() string

    /* BootWarningsIgnored names the boot warning codes a cli process leaves out of its journal; the framework's configuration reads them from config.CliBootWarningsIgnoredKey and ignores none by default. */
    BootWarningsIgnored() []string

    /* BootWarningsDisabled reports whether a cli process writes no boot warning at all; the framework's configuration reads it from config.CliBootWarningsDisabledKey, false by default. */
    BootWarningsDisabled() bool
}
