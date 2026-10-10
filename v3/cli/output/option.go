package output

type Option struct {
    Format         Format
    NoColor        bool
    VerbosityLevel int
    Verbose        bool
    Quiet          bool
    /* Deprecated: no printer reads it; it is withdrawn in v4. */
    Fields []string
    /* Deprecated: no printer reads it; it is withdrawn in v4. */
    SortKey        string
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
        Fields:         []string{},
        SortKey:        "",
        Order:          SortOrderAscending,
        Limit:          0,
        Offset:         0,
        TableMaxWidth:  0,
    }
}
