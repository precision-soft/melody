package output

import (
    "testing"
)

/** @info The block builder must not hold &Blocks[len-1]: a later AddBlock reallocates the slice, so rows added through the earlier builder would land in the old backing array and vanish. */
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
