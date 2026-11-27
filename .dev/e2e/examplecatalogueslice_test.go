package main

import (
    "testing"
)

const exampleProductListingSample = `product list: limit=5 views_window=24h0m0s
ID      |  NAME                         |  VIEWS  |  VIEWS_WINDOW  |  CREATED_AT
--------+-------------------------------+---------+----------------+---------------------
prod-1  |  DDR5 32GB Dual Kit 6000MT/s  |  12     |  3             |  2026-11-12 08:55:25
prod-10 |  Mechanical Keyboard TKL      |  7      |  7             |  2026-11-14 00:55:25
`

func TestExampleProductListColumns_ReadsTheRowByTheHeadersNames(t *testing.T) {
    columns := exampleProductListColumns(exampleProductListingSample, "prod-1")
    if nil == columns {
        t.Fatal("no row read for prod-1")
    }

    if "12" != columns["VIEWS"] || "3" != columns["VIEWS_WINDOW"] {
        t.Fatalf("read VIEWS %q and VIEWS_WINDOW %q, wanted 12 and 3", columns["VIEWS"], columns["VIEWS_WINDOW"])
    }
}

/* a product whose identifier starts another's must not be read in its place */
func TestExampleProductListColumns_MatchesTheWholeIdentifier(t *testing.T) {
    columns := exampleProductListColumns(exampleProductListingSample, "prod-10")
    if nil == columns || "7" != columns["VIEWS"] {
        t.Fatalf("read %v for prod-10, wanted its VIEWS 7", columns)
    }

    if nil != exampleProductListColumns(exampleProductListingSample, "prod-2") {
        t.Fatal("a product the listing does not print was answered a row")
    }
}

func TestExampleProductListColumns_AnswersNothingWithoutAViewColumn(t *testing.T) {
    listing := "ID      |  NAME\n--------+------\nprod-1  |  DDR5\n"

    if nil != exampleProductListColumns(listing, "prod-1") {
        t.Fatal("a listing without the view columns was answered a row")
    }
}
