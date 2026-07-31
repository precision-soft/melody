package output

import (
    "testing"

    "github.com/precision-soft/melody/internal/testhelper"
)

/* @info The block builder must not hold &Blocks[len-1]: a later AddBlock reallocates the slice, so rows added through the earlier builder would land in the old backing array and vanish. */
func TestTableBuilder_RowsSurviveALaterAddBlock(t *testing.T) {
    builder := NewTableBuilder()

    first := builder.AddBlock("first", []string{"a"})

    /* force the Blocks slice to grow past its initial capacity */
    for index := 0; index < 8; index++ {
        builder.AddBlock("filler", []string{"a"})
    }

    first.AddRow("kept")

    table := builder.Build()
    if 0 == len(table.Blocks) {
        t.Fatal("expected blocks")
    }

    if 1 != len(table.Blocks[0].Rows) {
        t.Fatalf("expected the row added through the first builder to survive, got %d rows", len(table.Blocks[0].Rows))
    }
    if "kept" != table.Blocks[0].Rows[0][0] {
        t.Fatalf("unexpected row content %v", table.Blocks[0].Rows[0])
    }
}

/* @info a row whose cell count disagrees with the declared columns is refused at the line that writes it: the printer sizes and prints by the columns alone, so a surplus cell silently never rendered and a missing one rendered as an empty cell nobody intended */
func TestTableBlockBuilder_AddRowPanicsOnACellCountMismatch(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"}).AddRow("a", "b", "c")
    }, "table row cell count does not match the block columns")

    testhelper.AssertPanicsWithError(t, func() {
        NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"}).AddRow("a")
    }, "table row cell count does not match the block columns")
}

/* @info the separator row is the one sanctioned exception: it is a single-token marker the printer expands to the full width */
func TestTableBlockBuilder_AddRowAdmitsTheSeparatorRow(t *testing.T) {
    table := NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"}).
        AddRow("a", "b").
        AddRow(TableRowSeparatorToken).
        AddRow("c", "d").
        owner.Build()

    if 3 != len(table.Blocks[0].Rows) {
        t.Fatalf("expected 3 rows, got %d", len(table.Blocks[0].Rows))
    }
}
