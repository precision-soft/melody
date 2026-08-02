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

/* @info a nil entry is refused where it is declared: read past the guard it dereferences on Names() while collecting the declared names, so a hole in a command's flag slice killed the boot inside the framework instead of naming the command that left it.
   The nil is the ONLY entry on purpose. Merged behind the standard set, this assertion passes over a guard armed for the wrong entry as well: the first standard flag then answers with the very same message, and the nil never reaches the line under test. */
func TestMergeFlags_PanicsOnANilFlag(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        MergeFlags(
            nil,
            []clicontract.Flag{nil},
        )
    }, "cli flag may not be nil in merge")
}

/* @info the same refusal where a command actually puts it — behind the standard flags, at an entry of its own */
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
