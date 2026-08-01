package pipeline

import (
    "testing"
)

/* @info the build report is what the debug command and the boot diagnostics read to explain a pipeline an operator did not get. Every accessor on it was at zero coverage, so a report that dropped a field on the way in — or handed out the slice it holds — would still have read as a working report through every path that only counts middlewares. */

func TestNewMiddlewareBuildReport_CarriesEveryFieldItWasGiven(t *testing.T) {
    inactive := []*InactiveMiddleware{
        NewInactiveMiddleware("compression", "not enabled in this environment"),
    }

    report := NewMiddlewareBuildReport(
        "web",
        "prod",
        []string{"security", "rate_limit"},
        inactive,
        []string{"audit"},
        true,
    )

    if "web" != report.RequestedGroup() {
        t.Fatalf("unexpected requested group: %q", report.RequestedGroup())
    }

    if "prod" != report.KernelEnv() {
        t.Fatalf("unexpected kernel environment: %q", report.KernelEnv())
    }

    if 2 != len(report.SelectedNames()) || "security" != report.SelectedNames()[0] {
        t.Fatalf("unexpected selected names: %v", report.SelectedNames())
    }

    if 1 != len(report.MissingReference()) || "audit" != report.MissingReference()[0] {
        t.Fatalf("unexpected missing references: %v", report.MissingReference())
    }

    if false == report.CycleDetected() {
        t.Fatalf("expected the cycle flag to be carried")
    }

    if 1 != len(report.Inactive()) {
        t.Fatalf("unexpected inactive list: %v", report.Inactive())
    }

    if "compression" != report.Inactive()[0].Name() {
        t.Fatalf("unexpected inactive name: %q", report.Inactive()[0].Name())
    }

    if "not enabled in this environment" != report.Inactive()[0].Reason() {
        t.Fatalf("unexpected inactive reason: %q", report.Inactive()[0].Reason())
    }
}

/* @info the cycle flag is the one field that turns a report into a refusal — Build fails the boot on it — so it has to survive both the constructor and the setter. A flag that silently stayed false would let a pipeline with a cycle boot and run in an order nothing chose. */

func TestMiddlewareBuildReport_CycleDetectedSurvivesBothWaysOfSettingIt(t *testing.T) {
    report := NewMiddlewareBuildReport("web", "dev", nil, nil, nil, false)

    if true == report.CycleDetected() {
        t.Fatalf("expected a report built without a cycle to report none")
    }

    report.SetCycleDetected(true)

    if false == report.CycleDetected() {
        t.Fatalf("expected the setter to reach the accessor")
    }

    report.SetCycleDetected(false)

    if true == report.CycleDetected() {
        t.Fatalf("expected the flag to be clearable")
    }
}

/* @info the constructor and the string-slice setters keep their own copies, so a caller that reuses the slice it passed cannot rewrite a report already handed to the diagnostics. The accessors copy on the way out for the same reason. */

func TestMiddlewareBuildReport_StringSlicesAreCopiedInBothDirections(t *testing.T) {
    selected := []string{"security"}
    missing := []string{"audit"}

    report := NewMiddlewareBuildReport("web", "prod", selected, nil, missing, false)

    selected[0] = "rewritten"
    missing[0] = "rewritten"

    if "security" != report.SelectedNames()[0] {
        t.Fatalf("expected the constructor to copy the selected names")
    }

    if "audit" != report.MissingReference()[0] {
        t.Fatalf("expected the constructor to copy the missing references")
    }

    report.SelectedNames()[0] = "rewritten"
    report.MissingReference()[0] = "rewritten"

    if "security" != report.SelectedNames()[0] {
        t.Fatalf("expected SelectedNames to hand out a copy")
    }

    if "audit" != report.MissingReference()[0] {
        t.Fatalf("expected MissingReference to hand out a copy")
    }

    setterSelected := []string{"rate_limit"}
    report.SetSelectedNames(setterSelected)
    setterSelected[0] = "rewritten"

    if "rate_limit" != report.SelectedNames()[0] {
        t.Fatalf("expected SetSelectedNames to copy the caller's slice")
    }

    setterMissing := []string{"tracing"}
    report.SetMissingReference(setterMissing)
    setterMissing[0] = "rewritten"

    if "tracing" != report.MissingReference()[0] {
        t.Fatalf("expected SetMissingReference to copy the caller's slice")
    }
}

/* @info a nil list stays nil rather than becoming an empty slice: the diagnostics distinguish "nothing was inactive" from "the field was never populated", and the copy helpers are the only place that distinction is made. */

func TestMiddlewareBuildReport_NilListsStayNil(t *testing.T) {
    report := NewMiddlewareBuildReport("web", "dev", nil, nil, nil, false)

    if nil != report.SelectedNames() {
        t.Fatalf("expected a nil selected list to stay nil, got: %v", report.SelectedNames())
    }

    if nil != report.MissingReference() {
        t.Fatalf("expected a nil missing list to stay nil, got: %v", report.MissingReference())
    }

    if nil != report.Inactive() {
        t.Fatalf("expected a nil inactive list to stay nil, got: %v", report.Inactive())
    }
}

/* @info SetInactive is the one setter on this type that does NOT copy, while SetSelectedNames and SetMissingReference both do and Inactive() copies on the way out. The asymmetry is pinned here rather than assumed: a caller that keeps the slice it passed can still rewrite the inactive list of a report the diagnostics already hold. */

func TestMiddlewareBuildReport_SetInactiveAliasesTheCallersSlice(t *testing.T) {
    report := NewMiddlewareBuildReport("web", "dev", nil, nil, nil, false)

    supplied := []*InactiveMiddleware{
        NewInactiveMiddleware("compression", "not enabled in this environment"),
    }

    report.SetInactive(supplied)

    supplied[0] = NewInactiveMiddleware("rewritten", "rewritten")

    if "rewritten" != report.Inactive()[0].Name() {
        t.Fatalf("expected SetInactive to alias the caller's slice, matching the behaviour as it stands today")
    }
}
