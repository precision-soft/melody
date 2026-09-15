package output

import (
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestMergeFlags_PanicsOnADuplicatedFlagName(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        MergeFlags(
            StandardFlags(),
            []clicontract.Flag{
                &clicontract.IntFlag{
                    Name:  FlagNameLimit,
                    Usage: "command specific limit",
                    Value: 100,
                },
            },
        )
    }, "cli flag name declared twice")
}

func TestMergeFlags_PanicsOnADuplicateInsideTheCommandSpecificFlags(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        MergeFlags(
            StandardFlags(),
            []clicontract.Flag{
                &clicontract.StringFlag{Name: "manager"},
                &clicontract.StringFlag{Name: "manager"},
            },
        )
    }, "cli flag name declared twice")
}

func TestMergeFlags_PanicsOnANilFlag(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        MergeFlags(
            nil,
            []clicontract.Flag{nil},
        )
    }, "cli flag may not be nil in merge")
}

func TestMergeFlags_PanicsOnANilFlagBehindTheStandardSet(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        MergeFlags(
            StandardFlags(),
            []clicontract.Flag{
                &clicontract.StringFlag{Name: "manager"},
                nil,
            },
        )
    }, "cli flag may not be nil in merge")
}

func TestMergeFlags_MergesDisjointFlagSets(t *testing.T) {
    merged := MergeFlags(
        StandardFlags(),
        []clicontract.Flag{
            &clicontract.StringFlag{Name: "manager"},
        },
    )

    if len(StandardFlags())+1 != len(merged) {
        t.Fatalf("expected the merged set to carry every flag, got %d", len(merged))
    }
}

func TestMergeFlags_ReturnsTheStandardFlagsWhenTheCommandAddsNone(t *testing.T) {
    merged := MergeFlags(StandardFlags(), nil)

    if len(StandardFlags()) != len(merged) {
        t.Fatalf("expected the standard set unchanged, got %d", len(merged))
    }
}
