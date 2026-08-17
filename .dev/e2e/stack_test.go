package main

import (
    "os"
    "strings"
    "testing"
)

/* stack.sh is bash dev tooling with no runtime harness in this repo, so these tests read the script and assert on
its TEXT: the helper each section has to call, the order two guards have to sit in, the diagnostic a failure path
has to name. That is a structural guarantee, not a behavioural one — nothing here runs stack.sh, so a script that
keeps the shape and loses the effect still passes. Executing the checks needs the whole compose stack and is
run.sh's and stack.sh's own job; what these tests buy is that a later edit cannot quietly drop the shape those
checks depend on. */
func readStackScript(t *testing.T) string {
    t.Helper()

    content, readErr := os.ReadFile("stack.sh")
    if nil != readErr {
        t.Fatalf("read stack.sh: %v", readErr)
    }

    return executableText(string(content))
}

func readCommonScript(t *testing.T) string {
    t.Helper()

    content, readErr := os.ReadFile("common.sh")
    if nil != readErr {
        t.Fatalf("read common.sh: %v", readErr)
    }

    return executableText(string(content))
}

/* executableText drops the comment lines before anything is searched. Every assertion below looks for a literal,
and a literal reads the same commented out as it does live: without this, commenting a check out — the cheapest
way to disable one — leaves each of these tests green. Blank lines take the comments' place so the line-order
assertions still see the region they expect. */
func executableText(source string) string {
    lines := strings.Split(source, "\n")

    for index, line := range lines {
        if true == strings.HasPrefix(strings.TrimSpace(line), "#") {
            lines[index] = ""
        }
    }

    return strings.Join(lines, "\n")
}

/* section_end takes its status as a literal argument, so a check script that calls it directly prints whatever
banner it was written with — green over a section that ran a check_fail. The status has to be derived from what
the section's checks did, which is what check_section_end does: it snapshots the failure counter at the start and
compares against it at the end. A literal status anywhere on that path is the defect. */
func TestStackScript_SectionBannersReportTheStatusTheirChecksEarned(t *testing.T) {
    script := readStackScript(t)

    for _, line := range strings.Split(script, "\n") {
        trimmed := strings.TrimSpace(line)

        if false == strings.HasPrefix(trimmed, "section_start ") && false == strings.HasPrefix(trimmed, "section_end ") {
            continue
        }

        t.Fatalf(
            "check sections must open with check_section_start and close with check_section_end (section_end takes the status as a literal, which is how a section that failed a check still printed a green banner): %q",
            trimmed,
        )
    }

    if false == strings.Contains(script, "check_section_end ") {
        t.Fatal("the script must close its sections with check_section_end")
    }

    common := readCommonScript(t)

    if false == strings.Contains(common, "CHECK_SECTION_FAILURE_BASELINE_INTEGER=\"${CHECK_FAILURE_COUNT_INTEGER}\"") {
        t.Fatal("check_section_start must snapshot the failure counter, or the section end has nothing to compare against")
    }

    if false == strings.Contains(common, "if [[ ${CHECK_FAILURE_COUNT_INTEGER} -gt ${CHECK_SECTION_FAILURE_BASELINE_INTEGER} ]]; then") {
        t.Fatal("check_section_end must compare the failure counter against the section's baseline to choose its status")
    }

    if false == strings.Contains(common, "STATUS_STRING=\"failure\"") {
        t.Fatal("check_section_end must be able to report a failure status")
    }

    /* the derivation above is worth nothing if the derived value is not what reaches the banner: every literal
       this test greps for survives a check_section_end that hands section_end a hardcoded status, so the call
       itself has to be pinned — the status argument must be the variable, and no call on this path may carry a
       status literal at all */
    if false == strings.Contains(common, "section_end \"${TITLE_STRING}\" \"${STATUS_STRING}\"") {
        t.Fatal("check_section_end must hand section_end the status it derived, not a literal")
    }

    for _, line := range append(strings.Split(common, "\n"), strings.Split(script, "\n")...) {
        trimmed := strings.TrimSpace(line)
        if false == strings.Contains(trimmed, "section_end ") {
            continue
        }

        if true == strings.Contains(trimmed, "\"success\"") || true == strings.Contains(trimmed, "\"failure\"") {
            t.Fatalf(
                "a section end on the check path must never be given a literal status — it is the derived one or nothing: %q",
                trimmed,
            )
        }
    }
}

/* no assertion can notice its own absence: with the checks uncounted, deleting an entire block ends the run in ALL
STACK CHECKS PASSED, simply doing less. The script therefore declares how many checks a complete run executes and
finish_checks compares that against the number that actually ran. */
func TestStackScript_AssertsTheExpectedCheckCount(t *testing.T) {
    script := readStackScript(t)

    if false == strings.Contains(script, "EXPECTED_CHECK_COUNT_INTEGER=") {
        t.Fatal("the script must declare the number of checks a complete run executes")
    }

    if false == strings.Contains(script, "finish_checks \"stack\" \"${EXPECTED_CHECK_COUNT_INTEGER}\"") {
        t.Fatal("finish_checks must be handed the expected check count, or the declaration asserts nothing")
    }

    common := readCommonScript(t)

    if false == strings.Contains(common, "CHECK_EXECUTED_COUNT_INTEGER=\"$((CHECK_EXECUTED_COUNT_INTEGER + 1))\"") {
        t.Fatal("check_pass and check_fail must count the checks they executed")
    }

    if false == strings.Contains(common, "${EXPECTED_COUNT_INTEGER} -ne ${CHECK_EXECUTED_COUNT_INTEGER}") {
        t.Fatal("finish_checks must compare the expected count against the executed count")
    }

    /* the message names both numbers, so the count to move to is in the failure itself */
    if false == strings.Contains(common, "expected ${EXPECTED_COUNT_INTEGER}") ||
        false == strings.Contains(common, "executed ${CHECK_EXECUTED_COUNT_INTEGER}") {
        t.Fatal("the count mismatch must report both the executed and the expected number")
    }
}

/* the two negatives in the secrets section assert that an entry does NOT carry a value. An entry that is missing
entirely — renamed parameter, crashed debug:parameters, dead docker exec — carries nothing either and satisfies
both on an empty string. Each has to establish the entry exists before concluding anything from its content. */
func TestStackScript_SecretNegativesRequireANonEmptyEntry(t *testing.T) {
    script := readStackScript(t)

    /* the section markers, not the bare titles: a title on its own also names the section banner and the header
       listing, so only the check_section_start call delimits the executable region */
    sectionIndex := strings.Index(script, "check_section_start \"PARAMETER SECRETS\"")
    if -1 == sectionIndex {
        t.Fatal("could not locate the parameter-secrets section")
    }

    endIndex := strings.Index(script[sectionIndex:], "check_section_start \"OPTIONAL ENV KEY\"")
    if -1 == endIndex {
        t.Fatal("could not locate the end of the parameter-secrets section")
    }

    region := script[sectionIndex : sectionIndex+endIndex]

    for _, testCase := range []struct {
        guard    string
        negative string
        name     string
    }{
        {guard: "[[ \"\" = \"${MYSQL_PASSWORD_ENTRY_STRING}\" ]]", negative: "grep -q 'melody'", name: "MYSQL_PASSWORD raw-credential"},
        {guard: "[[ \"\" = \"${DSN_ENTRY_STRING}\" ]]", negative: "grep -q 'tcp('", name: "assembled-dsn"},
    } {
        guardIndex := strings.Index(region, testCase.guard)
        negativeIndex := strings.Index(region, testCase.negative)

        if -1 == guardIndex {
            t.Fatalf("the %s negative must first assert the entry is non-empty, or a missing entry passes it", testCase.name)
        }
        if -1 == negativeIndex {
            t.Fatalf("could not locate the %s negative", testCase.name)
        }
        if guardIndex >= negativeIndex {
            t.Fatalf("the non-empty guard must precede (and short-circuit) the %s negative", testCase.name)
        }
    }
}

/* the V1 debug section carries the same leak negative for APP_API_TOKEN, under the same rule: an absent entry
carries no credential either, so the presence guard must short-circuit the negative. */
func TestStackScript_V1SecretNegativeRequiresANonEmptyEntry(t *testing.T) {
    script := readStackScript(t)

    sectionIndex := strings.Index(script, "check_section_start \"V1 DEBUG COMMANDS\"")
    if -1 == sectionIndex {
        t.Fatal("could not locate the v1 debug-commands section")
    }

    endIndex := strings.Index(script[sectionIndex:], "check_section_start \"V1 COMMAND OUTPUT ENVELOPE\"")
    if -1 == endIndex {
        t.Fatal("could not locate the end of the v1 debug-commands section")
    }

    region := script[sectionIndex : sectionIndex+endIndex]

    guardIndex := strings.Index(region, "[[ \"\" = \"${V1_API_TOKEN_ENTRY_STRING}\" ]]")
    negativeIndex := strings.Index(region, "grep -q 'example-api-token'")

    if -1 == guardIndex {
        t.Fatal("the APP_API_TOKEN negative must first assert the entry is non-empty, or a missing entry passes it")
    }
    if -1 == negativeIndex {
        t.Fatal("could not locate the APP_API_TOKEN leak negative")
    }
    if guardIndex >= negativeIndex {
        t.Fatal("the non-empty guard must precede (and short-circuit) the APP_API_TOKEN negative")
    }
}

/* on a cold go build cache the holder outruns the marker-wait budget; the exclusive section must emit a
distinct holder-marker-timeout diagnostic and NOT launch the contender, so a build delay is never misreported
as a mutual-exclusion violation the lock never committed. */
func TestStackScript_ExclusiveMarkerTimeoutFailsLoudly(t *testing.T) {
    script := readStackScript(t)

    if false == strings.Contains(script, "holder_marker_timeout=1") {
        t.Fatal("the exclusive section must emit a distinct holder_marker_timeout diagnostic when the start marker never appears within the wait budget")
    }

    if false == strings.Contains(script, "never printed the start marker") {
        t.Fatal("the exclusive section must fail with a distinct 'holder never printed the start marker' message instead of the mutual-exclusion assertions")
    }

    /* the timeout branch must guard the exclusivity assertions, so an empty holder log does not trip them */
    timeoutGuardIndex := strings.Index(script, "grep -q 'holder_marker_timeout=1'")
    startedCountIndex := strings.Index(script, "STARTED_COUNT_INTEGER=")
    if -1 == timeoutGuardIndex || -1 == startedCountIndex {
        t.Fatal("could not locate the timeout guard and the started-count assertion")
    }
    if timeoutGuardIndex >= startedCountIndex {
        t.Fatal("the holder-marker-timeout guard must precede (and short-circuit) the mutual-exclusion assertions")
    }
}

/* a prior run killed before its EXIT trap ran leaves MELODY_PROCESS_ROLE=web in .env.local on the repo
bind mount; the default-role check must clear it first (mirroring the rm -f hygiene the other sections use) so
a stale role cannot poison the default-role assertion. */
func TestStackScript_ProcessRoleClearsStaleEnvLocalBeforeDefaultCheck(t *testing.T) {
    script := readStackScript(t)

    trapMarker := "trap restore_example_env_local EXIT"
    trapIndex := strings.Index(script, trapMarker)
    if -1 == trapIndex {
        t.Fatal("could not locate the process-role EXIT trap")
    }

    defaultCheckIndex := strings.Index(script, "DEFAULT_ROLE_STRING=")
    if -1 == defaultCheckIndex {
        t.Fatal("could not locate the default-role check")
    }

    /* the cleanup call has to sit strictly between arming the trap and the first default-role probe: anywhere
       later and the probe has already read whatever role the previous run left behind */
    region := script[trapIndex+len(trapMarker) : defaultCheckIndex]
    if false == strings.Contains(region, "restore_example_env_local") {
        t.Fatal("the default-role check must clear a stale .env.local (call restore_example_env_local) before probing the role")
    }
}

/* the message-bus check proves the dispatch went to the async transport by reading the handler's marker BEFORE the
consumer runs: it must not have grown. Drop that middle reading and the section still goes green against a bus that
handles every message inline — the +1 after the consume would simply be a message the drain missed. The drain
itself is load-bearing for the same reason, so both readings and the drain are pinned here. */
func TestStackScript_MessageBusProvesTheDispatchWasNotHandledInline(t *testing.T) {
    script := readStackScript(t)

    drainIndex := strings.Index(script, "--transport async --limit 0")
    if -1 == drainIndex {
        t.Fatal("the message-bus section must drain the queue with an unlimited consume before it measures anything")
    }

    beforeIndex := strings.Index(script, "before_handled=")
    inlineIndex := strings.Index(script, "inline_handled=")
    afterIndex := strings.Index(script, "after_handled=")
    if -1 == beforeIndex || -1 == inlineIndex || -1 == afterIndex {
        t.Fatal("the message-bus section must read the handler marker before the dispatch, after the dispatch and after the consume")
    }

    /* the drain has to precede the baseline, or the baseline counts a message an earlier run left behind */
    if drainIndex >= beforeIndex {
        t.Fatal("the queue drain must run before the baseline marker reading")
    }
    if beforeIndex >= inlineIndex || inlineIndex >= afterIndex {
        t.Fatal("the three marker readings must appear in order: baseline, post-dispatch, post-consume")
    }

    if false == strings.Contains(script, "handled inline") {
        t.Fatal("the message-bus section must fail with a distinct 'handled inline' diagnostic when the marker grew during the http dispatch")
    }
}

/* `timeout go run .` signals go run and ORPHANS the compiled binary it started, leaving a consumer that keeps
eating messages out of the queue for the rest of the session — which then stands in for the message this check
dispatched. Both consumers must therefore run in their own process group and be signalled as a group. */
func TestStackScript_MessageBusConsumersAreSignalledAsAProcessGroup(t *testing.T) {
    script := readStackScript(t)

    sectionIndex := strings.Index(script, "MESSAGE BUS ASYNC TRANSPORT")
    if -1 == sectionIndex {
        t.Fatal("could not locate the message-bus section")
    }

    endIndex := strings.Index(script[sectionIndex:], "ENCRYPT FACTORY COMMAND")
    if -1 == endIndex {
        t.Fatal("could not locate the end of the message-bus section")
    }

    region := script[sectionIndex : sectionIndex+endIndex]

    /* the prose above the check explains WHY timeout is unusable here, and readStackScript has already dropped it
       — otherwise the explanation itself would trip the assertion it exists to justify */
    for _, line := range strings.Split(region, "\n") {
        trimmed := strings.TrimSpace(line)

        if true == strings.Contains(trimmed, "timeout ") {
            t.Fatalf(
                "the message-bus consumers must not be bounded with timeout (it signals go run and orphans the compiled child, which then keeps consuming the queue): %q",
                trimmed,
            )
        }
    }
    /* every background launch is checked, not the region as a whole. A `strings.Contains` over the region is
       satisfied by the first consumer's guard and says nothing about the second — and the second is the long one,
       the very consumer the prose above describes as orphaning itself and going on eating the queue. Removing
       `set -m` from it alone left the old assertion green. */
    regionLineList := strings.Split(region, "\n")

    launchCountInteger := 0
    for lineIndex, line := range regionLineList {
        trimmed := strings.TrimSpace(line)

        if false == strings.Contains(trimmed, "melody:messagebus:consume") || false == strings.HasSuffix(trimmed, "&") {
            continue
        }

        launchCountInteger = launchCountInteger + 1

        precedingIndex := lineIndex - 1
        for 0 <= precedingIndex {
            preceding := strings.TrimSpace(regionLineList[precedingIndex])
            if "" != preceding && false == strings.HasPrefix(preceding, "#") {
                break
            }
            precedingIndex = precedingIndex - 1
        }

        if 0 > precedingIndex || "set -m" != strings.TrimSpace(regionLineList[precedingIndex]) {
            t.Fatalf(
                "the consumer started at %q is not preceded by set -m, so it does not get its own process group and the compiled child it leaves behind cannot be signalled",
                trimmed,
            )
        }
    }

    if 2 > launchCountInteger {
        t.Fatalf("expected the drain and the consume consumers to be started as background jobs, found %d", launchCountInteger)
    }

    if launchCountInteger > strings.Count(region, "kill -TERM -- -") {
        t.Fatalf(
            "%d consumers are started but only %d process-group terminations are issued; each must be terminated by group, not by the go run pid alone",
            launchCountInteger,
            strings.Count(region, "kill -TERM -- -"),
        )
    }
}
