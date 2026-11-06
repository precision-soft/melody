package output

import (
    "github.com/precision-soft/melody/v4/bound"
)

/* Option is what a command's output is rendered with. The zero value is the default: a table, colour on, verbosity 0, ascending order, no item limit, no offset and a table 120 cells wide. */
type Option struct {
    /* Format is table, json or json-pretty; "" reads as table, as does a format no printer knows. */
    Format         Format
    NoColor        bool
    VerbosityLevel int
    Verbose        bool
    Quiet          bool
    /* Order is asc or desc; "" reads as asc, as does an order no printer knows. */
    Order SortOrder
    /* Limit caps the items a command lists: absent is no cap, a value is at least 0, unlimited admitted. */
    Limit bound.Count
    /* Offset skips the first items: absent is 0, a value is at least 0, unlimited refused. */
    Offset bound.Count
    /* TableMaxWidth caps a table's width in characters: absent is 120, a value is at least 1, unlimited lets the table be as wide as its content. */
    TableMaxWidth bound.Count
}

var (
    OptionLimitRule = bound.Rule[int]{
        Name:             "Option.Limit",
        DefaultUnlimited: true,
        Minimum:          0,
    }

    OptionOffsetRule = bound.Rule[int]{
        Name:    "Option.Offset",
        Default: 0,
        Minimum: 0,
    }

    OptionTableMaxWidthRule = bound.Rule[int]{
        Name:              "Option.TableMaxWidth",
        Default:           defaultTableMaxWidth,
        Minimum:           1,
        UnlimitedAdmitted: true,
    }
)
