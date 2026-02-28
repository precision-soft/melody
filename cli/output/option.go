package output

type Option struct {
    Format         Format
    NoColor        bool
    VerbosityLevel int
    Verbose        bool
    Quiet          bool
    Fields         []string
    SortKey        string
    Order          SortOrder
    Limit          int
    Offset         int
}

func DefaultOption() Option {
    return Option{
        Format:         FormatTable,
        NoColor:        false,
        VerbosityLevel: 0,
        Verbose:        false,
        Quiet:          false,
        Fields:         []string{},
        SortKey:        "",
        Order:          SortOrderAscending,
        Limit:          0,
        Offset:         0,
    }
}
