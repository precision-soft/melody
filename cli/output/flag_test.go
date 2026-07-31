package output

import (
    "testing"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/internal/testhelper"
)

/* @info a duplicated name is refused at the line that declares it: the flag parser resolves a name to the FIRST declaration, so a command-specific flag reusing a standard name — its default, its validator — was silently inert, with the help output listing the name twice as the only trace */
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

/* @info two command-specific flags colliding with each other are the same mistake made closer to home */
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
