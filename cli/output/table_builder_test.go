package output

import (
    "testing"

    "github.com/precision-soft/melody/internal/testhelper"
)

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

func TestTableBlockBuilder_AddRowPanicsOnACellCountMismatch(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"}).AddRow("a", "b", "c")
    }, "table row cell count does not match the block columns")

    testhelper.AssertPanicsWithError(t, func() {
        NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"}).AddRow("a")
    }, "table row cell count does not match the block columns")
}

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
