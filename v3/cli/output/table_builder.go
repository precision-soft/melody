package output

type TableBuilder struct {
    table TableData
}

func NewTableBuilder() *TableBuilder {
    return &TableBuilder{
        table: TableData{
            SummaryLines: []string{},
            Blocks:       []TableBlock{},
        },
    }
}

func (instance *TableBuilder) AddSummaryLine(line string) *TableBuilder {
    instance.table.SummaryLines = append(instance.table.SummaryLines, line)
    return instance
}

func (instance *TableBuilder) AddBlock(
    title string,
    columns []string,
) *TableBlockBuilder {
    block := TableBlock{
        Title:   title,
        Columns: columns,
        Rows:    [][]string{},
    }

    instance.table.Blocks = append(instance.table.Blocks, block)

    /* hold the owner and the index, never a pointer into the slice: a later AddBlock can reallocate Blocks, and a builder pointing at the old backing array would silently write its rows into memory nobody reads */
    return &TableBlockBuilder{
        owner: instance,
        index: len(instance.table.Blocks) - 1,
    }
}

func (instance *TableBuilder) Build() *TableData {
    return &instance.table
}

type TableBlockBuilder struct {
    owner *TableBuilder
    index int
}

func (instance *TableBlockBuilder) AddRow(cells ...string) *TableBlockBuilder {
    block := &instance.owner.table.Blocks[instance.index]
    block.Rows = append(block.Rows, cells)

    return instance
}
