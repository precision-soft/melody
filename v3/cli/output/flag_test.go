package output

import (
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

/* the flag parser resolves a name to the FIRST declaration, so a duplicate reaching it is inert rather than an error, which is why the refusal belongs at the line that declares it */
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

/* the nil is the ONLY entry on purpose: merged behind the standard set, this assertion would pass over a guard armed for the wrong entry as well, because the first standard flag answers with the very same message and the nil never reaches the line under test. */
func TestMergeFlags_PanicsOnANilFlag(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        MergeFlags(
            nil,
            []clicontract.Flag{nil},
        )
    }, "cli flag may not be nil in merge")
}

/* the realistic shape, behind the standard flags — kept as a test, but the sibling above is the proof: here the first standard flag can answer instead. */
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
