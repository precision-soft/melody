package contract

import (
    urfavecli "github.com/urfave/cli/v3"
)

type CommandContext = urfavecli.Command

type Flag = urfavecli.Flag
type StringFlag = urfavecli.StringFlag
type StringSliceFlag = urfavecli.StringSliceFlag
type BoolFlag = urfavecli.BoolFlag
type IntFlag = urfavecli.IntFlag
