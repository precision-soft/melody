package output

import (
    "github.com/precision-soft/melody/exception"
)

/* TableBuilder is not safe for concurrent use: a command that assembles its table from parallel work funnels the rows through one goroutine — a channel the workers send to — rather than sharing the builder between them. */
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

    /* a row whose cell count disagrees with the declared columns is refused at the line that writes it: the printer sizes and prints by the columns alone, so a surplus cell would silently never render and a missing one would render as an empty cell nobody intended — a report reading complete while it is not. The separator row is the one sanctioned exception: it is a single-token marker the printer expands to the full width. */
    if len(cells) != len(block.Columns) && false == isSeparatorCells(cells) {
        exception.Panic(
            exception.NewError(
                "table row cell count does not match the block columns",
                map[string]any{
                    "blockTitle":  block.Title,
                    "columnCount": len(block.Columns),
                    "cellCount":   len(cells),
                },
                nil,
            ),
        )
    }

    block.Rows = append(block.Rows, cells)

    return instance
}

func isSeparatorCells(cells []string) bool {
    if 1 != len(cells) {
        return false
    }

    return TableRowSeparatorToken == cells[0]
}
