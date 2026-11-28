package output

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func TestTableBuilder_RowsSurviveALaterAddBlock(t *testing.T) {
    builder := NewTableBuilder()

    first := builder.AddBlock("first", []string{"a"})

    /* force the Blocks slice to grow past its initial capacity */
    for index := 0; index < 8; index++ {
        builder.AddBlock("filler", []string{"a"})
    }

    first.AddRow([]string{"kept"})

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

func TestTableBlockBuilder_AddRowRefusesACellCountOffTheColumns(t *testing.T) {
    cases := []struct {
        name      string
        cells     []string
        cellCount int
    }{
        {name: "a long row", cells: []string{"a", "b", "c"}, cellCount: 3},
        {name: "a short row", cells: []string{"a"}, cellCount: 1},
        {name: "a nil row", cells: nil, cellCount: 0},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            recovered := recoverAddRowPanic(func() {
                NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"}).AddRow(testCase.cells)
            })

            refusal, isRefusal := recovered.(*exception.Error)
            if false == isRefusal {
                t.Fatalf("expected an *exception.Error panic, got %T: %v", recovered, recovered)
            }
            if "table row cell count does not match the block columns" != refusal.Message() {
                t.Fatalf("unexpected message %q", refusal.Message())
            }

            context := refusal.Context()
            if "BLOCK" != context["blockTitle"] || 2 != context["columnCount"] || testCase.cellCount != context["cellCount"] {
                t.Fatalf("unexpected context %v", context)
            }
        })
    }
}

func TestTableBlockBuilder_AddRowKeepsACopyOfItsCells(t *testing.T) {
    cells := []string{"a", "b"}

    block := NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"})
    block.AddRow(cells)

    cells[0] = "changed"
    block.AddRow(cells)

    rows := block.owner.Build().Blocks[0].Rows
    if "a" != rows[0][0] || "changed" != rows[1][0] {
        t.Fatalf("expected the first row to keep its own cells, got %v", rows)
    }
}

func recoverAddRowPanic(callback func()) (recovered any) {
    defer func() {
        recovered = recover()
    }()

    callback()

    return nil
}

func TestTableBlockBuilder_AddRowAdmitsTheSeparatorRow(t *testing.T) {
    table := NewTableBuilder().AddBlock("BLOCK", []string{"name", "description"}).
        AddRow([]string{"a", "b"}).
        AddRow([]string{TableRowSeparatorToken}).
        AddRow([]string{"c", "d"}).
        owner.Build()

    if 3 != len(table.Blocks[0].Rows) {
        t.Fatalf("expected 3 rows, got %d", len(table.Blocks[0].Rows))
    }
}
