package cli

import (
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    urfavecli "github.com/urfave/cli/v3"
)

/* the engine's version flag carries the alias -v, which melody spells for verbosity */
func init() {
    urfavecli.VersionFlag = &clicontract.BoolFlag{
        Name:        "version",
        Usage:       "print the version",
        HideDefault: true,
        Local:       true,
    }
}
