package cli

import (
    "errors"
    "net/http"
    "strings"
    "testing"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

func TestCatalogReportRefreshCommandDrivesLockRefreshArchiveExportInThatOrder(t *testing.T) {
    fixture := newRefreshFixture(t, refreshFixtureOption{})

    output, runErr := runRefresh(t, fixture)
    if nil != runErr {
        t.Fatalf("expected the refresh to succeed, got %v", runErr)
    }

    wanted := []string{"acquire", "refresh", "archive", "export", "release"}
    if strings.Join(wanted, ",") != strings.Join(fixture.sequence.recorded(), ",") {
        t.Fatalf("expected the run to drive %v, it drove %v", wanted, fixture.sequence.recorded())
    }

    if false == strings.Contains(output, "true") || false == strings.Contains(output, "ARCHIVED") {
        t.Fatalf("expected the table to report the export and the archive, got %q", output)
    }
}

func TestCatalogReportRefreshCommandSkipsTheWholeRunWhenTheLockIsHeldElsewhere(t *testing.T) {
    sequenceHolder := &refreshSequence{}
    fixture := newRefreshFixture(t, refreshFixtureOption{
        lockerProvider: func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            return &sequenceLocker{sequence: sequenceHolder, inner: melodylock.NewInMemoryLocker(melodyclock.NewSystemClock()), heldAway: true}, nil
        },
    })

    output, runErr := runRefresh(t, fixture)
    if nil != runErr {
        t.Fatalf("expected a lost lock to be a zero exit, got %v", runErr)
    }

    if 0 != fixture.sequence.count("refresh") || 0 != fixture.sinkHits || 0 != fixture.archive.appended() {
        t.Fatalf("expected no reading, no export and no row behind a lost lock, got refresh=%d export=%d archive=%d", fixture.sequence.count("refresh"), fixture.sinkHits, fixture.archive.appended())
    }

    if false == strings.Contains(output, "another process is taking this reading") {
        t.Fatalf("expected the command to say another process is taking the reading, got %q", output)
    }
}

func TestCatalogReportRefreshCommandHandsBackAnUnreachableArchiveAfterReadingAndExporting(t *testing.T) {
    refusal := errors.New("dial tcp 172.18.0.10:5432: connect: connection refused")
    fixture := newRefreshFixture(t, refreshFixtureOption{
        lockerProvider: func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            return nil, exception.NewError("the archive database at postgres:5432/melody_example_v3 could not be opened", nil, refusal)
        },
    })

    output, runErr := runRefresh(t, fixture)
    if nil == runErr {
        t.Fatalf("expected the unreachable archive to fail the run")
    }

    if false == strings.Contains(runErr.Error(), "the archive database at postgres:5432/melody_example_v3 could not be opened") {
        t.Fatalf("expected the failure to name the archive and where it is, got %v", runErr)
    }

    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the driver's refusal to stay the cause, got %v", runErr)
    }

    if 1 != fixture.sequence.count("refresh") || 1 != fixture.sinkHits || 0 != fixture.archive.appended() {
        t.Fatalf("expected the reading taken and exported and nothing archived, got refresh=%d export=%d archive=%d", fixture.sequence.count("refresh"), fixture.sinkHits, fixture.archive.appended())
    }

    if false == strings.Contains(output, "the reading was taken and exported; the archive did not record it") {
        t.Fatalf("expected the operator to be told what happened, got %q", output)
    }
}

func TestCatalogReportRefreshCommandRunsWithoutAnArchiveWhenNoneIsWired(t *testing.T) {
    fixture := newRefreshFixture(t, refreshFixtureOption{withoutLocker: true})

    output, runErr := runRefresh(t, fixture)
    if nil != runErr {
        t.Fatalf("expected the refresh to run without an archive, got %v", runErr)
    }

    if 1 != fixture.sinkHits || 0 != fixture.archive.appended() {
        t.Fatalf("expected one export and no row, got export=%d archive=%d", fixture.sinkHits, fixture.archive.appended())
    }

    if false == strings.Contains(output, "false") {
        t.Fatalf("expected the table to report the archive as not written, got %q", output)
    }
}

func TestCatalogReportRefreshCommandArchivesTheReadingEvenWhenTheSinkRefusesIt(t *testing.T) {
    fixture := newRefreshFixture(t, refreshFixtureOption{sinkStatus: http.StatusInternalServerError})

    output, runErr := runRefresh(t, fixture)
    if nil == runErr {
        t.Fatalf("expected the sink's refusal to take the exit code")
    }

    if 1 != fixture.archive.appended() {
        t.Fatalf("expected the reading to be archived before the sink was told, got %d rows", fixture.archive.appended())
    }

    if 0 > fixture.sequence.indexOf("archive") || fixture.sequence.indexOf("archive") > fixture.sequence.indexOf("export") {
        t.Fatalf("expected the archive before the export, the run drove %v", fixture.sequence.recorded())
    }

    if false == strings.Contains(output, "the archive holds this reading; the sink did not receive it") {
        t.Fatalf("expected the operator to be told the archive holds the reading, got %q", output)
    }
}

func TestCatalogReportRefreshCommandHandsBackTheArchivesOwnRefusalUnwrapped(t *testing.T) {
    own := exception.NewError("the archive database at postgres:1/melody_example_v3 could not be opened", nil, errors.New("connection refused"))
    fixture := newRefreshFixture(t, refreshFixtureOption{
        lockerProvider: func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            return nil, own
        },
    })

    _, runErr := runRefresh(t, fixture)
    if own != runErr {
        t.Fatalf("expected the archive's own refusal to be handed back as it is, got %v", runErr)
    }
}
