package contract

import (
    urfavecli "github.com/urfave/cli/v3"
)

/* CommandContext is the parsed command a command's Run receives: its flags, read by name, and its positional arguments through Args(). The flags a command declares are the engine's own types under the names below. */
type CommandContext = urfavecli.Command

type Flag = urfavecli.Flag
type StringFlag = urfavecli.StringFlag
type StringSliceFlag = urfavecli.StringSliceFlag
type BoolFlag = urfavecli.BoolFlag
type IntFlag = urfavecli.IntFlag
