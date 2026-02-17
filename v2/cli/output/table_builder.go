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

	return &TableBlockBuilder{
		block: &instance.table.Blocks[len(instance.table.Blocks)-1],
	}
}

func (instance *TableBuilder) Build() *TableData {
	return &instance.table
}

type TableBlockBuilder struct {
	block *TableBlock
}

func (instance *TableBlockBuilder) AddRow(cells ...string) *TableBlockBuilder {
	instance.block.Rows = append(instance.block.Rows, cells)
	return instance
}
