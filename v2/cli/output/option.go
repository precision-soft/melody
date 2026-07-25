package output

type Option struct {
    Format         Format
    NoColor        bool
    VerbosityLevel int
    Verbose        bool
    Quiet          bool
    Order          SortOrder
    Limit          int
    Offset         int
    TableMaxWidth  int
}

func DefaultOption() Option {
    return Option{
        Format:         FormatTable,
        NoColor:        false,
        VerbosityLevel: 0,
        Verbose:        false,
        Quiet:          false,
        Order:          SortOrderAscending,
        Limit:          0,
        Offset:         0,
        TableMaxWidth:  0,
    }
}
