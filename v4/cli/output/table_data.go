package output

type TableBlock struct {
    Title   string
    Columns []string
    Rows    [][]string
}

type TableData struct {
    SummaryLines []string
    Blocks       []TableBlock
}
