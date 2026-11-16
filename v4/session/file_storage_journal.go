package session

import (
    "bytes"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "io"
    "os"
    "path/filepath"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* DefaultJournalMaxBytes is the journal size past which a Save or a Delete compacts it into the snapshot. */
const DefaultJournalMaxBytes = 8 << 20

const (
    sessionJournalSuffix = ".journal"

    sessionJournalOperationHeader = "header"
    sessionJournalOperationSave   = "save"
    sessionJournalOperationDelete = "delete"

    sessionJournalVersion = 1
)

var fileStorageJournalMaxBytesRule = bound.Rule[int]{
    Name:    "FileStorageConfig.JournalMaxBytes",
    Default: DefaultJournalMaxBytes,
    Minimum: 64 << 10,
}

/* a test seam observes the steps in order, or panics at one to stop the storage as a kill would */
type journalPoint string

const (
    journalPointRecordWritten   journalPoint = "record written"
    journalPointRecordSynced    journalPoint = "record synced"
    journalPointSnapshotRenamed journalPoint = "snapshot renamed"
    journalPointJournalCut      journalPoint = "journal cut"
)

/* a record carries the whole value, so a replay is idempotent */
type sessionJournalRecord struct {
    Operation string         `json:"operation"`
    Id        string         `json:"id,omitempty"`
    ExpiresAt int64          `json:"expiresAt,omitempty"`
    Data      map[string]any `json:"data,omitempty"`
}

/* the header names the snapshot the journal extends, so a journal left behind a snapshot another build rewrote is not replayed over it */
type sessionJournalHeader struct {
    Operation string `json:"operation"`
    Version   int    `json:"version"`
    Snapshot  string `json:"snapshot"`
}

type sessionJournalReplay struct {
    replayed  int
    tailCut   bool
    stale     bool
    discarded int
    reset     bool
}

/* a basename past 200 bytes is digested, as in sessionTemporaryPrefix, so the suffix stays within one 255-byte component */
func sessionJournalPath(path string) string {
    base := filepath.Base(path)
    if 200 < len(base) {
        digest := sha256.Sum256([]byte(base))
        base = ".melody-session-" + hex.EncodeToString(digest[:])
    }

    return filepath.Join(filepath.Dir(path), base+sessionJournalSuffix)
}

/* a new journal's directory entry is synced, so its first record does not land in a file a crash can drop */
func openSessionJournal(path string, snapshotDigest string, sessionById map[string]fileSessionEntry) (*os.File, sessionJournalReplay, error) {
    journalPath := sessionJournalPath(path)

    _, statErr := os.Stat(journalPath)
    created := true == os.IsNotExist(statErr)

    journal, openErr := os.OpenFile(journalPath, os.O_RDWR|os.O_CREATE, 0600)
    if nil != openErr {
        return nil, sessionJournalReplay{}, exception.NewError(
            "failed to open session storage journal",
            exceptioncontract.Context{
                "path": journalPath,
            },
            openErr,
        )
    }

    if true == created {
        if syncErr := syncSessionDirectory(filepath.Dir(path)); nil != syncErr {
            _ = journal.Close()

            return nil, sessionJournalReplay{}, exception.NewError(
                "failed to sync the directory of a new session storage journal",
                exceptioncontract.Context{
                    "path": journalPath,
                },
                syncErr,
            )
        }
    }

    replay, replayErr := replaySessionJournal(journal, snapshotDigest, sessionById)
    if nil != replayErr {
        _ = journal.Close()

        return nil, sessionJournalReplay{}, replayErr
    }

    return journal, replay, nil
}

/* a kill mid-append leaves a line without its newline or one that does not decode: the replay cuts there */
func replaySessionJournal(journal *os.File, snapshotDigest string, sessionById map[string]fileSessionEntry) (sessionJournalReplay, error) {
    if _, seekErr := journal.Seek(0, io.SeekStart); nil != seekErr {
        return sessionJournalReplay{}, exception.NewError("failed to seek session storage journal", nil, seekErr)
    }

    content, readErr := io.ReadAll(journal)
    if nil != readErr {
        return sessionJournalReplay{}, exception.NewError("failed to read session storage journal", nil, readErr)
    }

    headerEnd := bytes.IndexByte(content, '\n')
    header := sessionJournalHeader{}

    if -1 == headerEnd || nil != json.Unmarshal(content[:headerEnd], &header) || sessionJournalOperationHeader != header.Operation {
        return sessionJournalReplay{tailCut: 0 < len(content), reset: true}, nil
    }

    if sessionJournalVersion != header.Version {
        return sessionJournalReplay{}, exception.NewError(
            "session storage journal has a format this build does not read",
            exceptioncontract.Context{
                "version":         header.Version,
                "readableVersion": sessionJournalVersion,
            },
            nil,
        )
    }

    if snapshotDigest != header.Snapshot {
        return sessionJournalReplay{stale: true, discarded: bytes.Count(content[headerEnd+1:], []byte{'\n'}), reset: true}, nil
    }

    replay := sessionJournalReplay{}
    validLength := headerEnd + 1

    for validLength < len(content) {
        newlineIndex := bytes.IndexByte(content[validLength:], '\n')
        if -1 == newlineIndex {
            break
        }

        line := content[validLength : validLength+newlineIndex]

        record := sessionJournalRecord{}
        if decodeErr := json.Unmarshal(line, &record); nil != decodeErr || false == applySessionJournalRecord(record, sessionById) {
            break
        }

        validLength += newlineIndex + 1
        replay.replayed++
    }

    if validLength == len(content) {
        return replay, nil
    }

    replay.tailCut = true

    if truncateErr := journal.Truncate(int64(validLength)); nil != truncateErr {
        return sessionJournalReplay{}, exception.NewError("failed to cut the torn tail of the session storage journal", nil, truncateErr)
    }

    if syncErr := journal.Sync(); nil != syncErr {
        return sessionJournalReplay{}, exception.NewError("failed to sync the session storage journal after cutting its torn tail", nil, syncErr)
    }

    return replay, nil
}

func encodeSessionJournalHeader(snapshotDigest string) []byte {
    encoded, _ := json.Marshal(sessionJournalHeader{
        Operation: sessionJournalOperationHeader,
        Version:   sessionJournalVersion,
        Snapshot:  snapshotDigest,
    })

    return append(encoded, '\n')
}

/* the digest of the snapshot's bytes on disk, empty when there is none */
func sessionSnapshotDigest(path string) (string, error) {
    content, readErr := os.ReadFile(path)
    if nil != readErr {
        if true == os.IsNotExist(readErr) {
            return "", nil
        }

        return "", exception.NewError("failed to read session storage file", exceptioncontract.Context{"path": path}, readErr)
    }

    return digestOf(content), nil
}

func digestOf(content []byte) string {
    digest := sha256.Sum256(content)

    return hex.EncodeToString(digest[:])
}

/* a record no build writes reads as the torn tail */
func applySessionJournalRecord(record sessionJournalRecord, sessionById map[string]fileSessionEntry) bool {
    if "" == record.Id {
        return false
    }

    switch record.Operation {
    case sessionJournalOperationSave:
        data := record.Data
        if nil == data {
            data = make(map[string]any)
        }

        sessionById[record.Id] = fileSessionEntry{
            Data:      data,
            ExpiresAt: record.ExpiresAt,
        }

        return true
    case sessionJournalOperationDelete:
        delete(sessionById, record.Id)

        return true
    default:
        return false
    }
}

/* a failed append is cut back to the previous length; when that cut fails too, the next write compacts first */
func (instance *FileStorage) appendJournalRecordLocked(record sessionJournalRecord) error {
    encoded, encodeErr := json.Marshal(record)
    if nil != encodeErr {
        return exception.NewError("failed to encode session storage journal record", nil, encodeErr)
    }

    encoded = append(encoded, '\n')

    previousSize := instance.journalSize

    writeErr := instance.writeJournalRecordLocked(encoded)
    if nil != writeErr {
        if truncateErr := instance.journal.Truncate(previousSize); nil != truncateErr {
            instance.journalNeedsCompaction = true
        } else if syncErr := instance.journal.Sync(); nil != syncErr {
            instance.journalNeedsCompaction = true
        }

        return writeErr
    }

    instance.journalSize = previousSize + int64(len(encoded))

    return nil
}

func (instance *FileStorage) writeJournalRecordLocked(encoded []byte) error {
    if _, writeErr := instance.journal.WriteAt(encoded, instance.journalSize); nil != writeErr {
        return exception.NewError("failed to write session storage journal record", nil, writeErr)
    }

    instance.reachJournalPoint(journalPointRecordWritten)

    if syncErr := instance.journal.Sync(); nil != syncErr {
        return exception.NewError("failed to sync session storage journal record", nil, syncErr)
    }

    instance.reachJournalPoint(journalPointRecordSynced)

    return nil
}

/* a write runs a pending compaction before it touches the map, so a refusal leaves both the map and the disk as they were */
func (instance *FileStorage) compactPendingLocked() error {
    if false == instance.journalNeedsCompaction {
        return nil
    }

    return instance.compactLocked()
}

/* the journal is reset only once the snapshot is durable, so a crash between the two leaves a header naming the previous snapshot, which the next construction discards as the snapshot already holds its records */
func (instance *FileStorage) compactLocked() error {
    instance.purgeExpiredLocked()

    encoded, encodeErr := encodeSessionSnapshot(instance.sessionById)
    if nil != encodeErr {
        return encodeErr
    }

    if snapshotErr := writeSessionBytesAtomically(instance.path, encoded); nil != snapshotErr {
        instance.journalNeedsCompaction = true

        return snapshotErr
    }

    instance.reachJournalPoint(journalPointSnapshotRenamed)

    if resetErr := instance.resetJournalLocked(digestOf(encoded)); nil != resetErr {
        instance.journalNeedsCompaction = true

        return persistedDespiteFlushFailure("failed to reset the session storage journal after compaction", nil, resetErr)
    }

    instance.journalNeedsCompaction = false

    instance.reachJournalPoint(journalPointJournalCut)

    return nil
}

/* resetJournalLocked leaves the journal holding only a header naming the snapshot on disk */
func (instance *FileStorage) resetJournalLocked(snapshotDigest string) error {
    if truncateErr := instance.journal.Truncate(0); nil != truncateErr {
        return truncateErr
    }

    header := encodeSessionJournalHeader(snapshotDigest)

    if _, writeErr := instance.journal.WriteAt(header, 0); nil != writeErr {
        return writeErr
    }

    if syncErr := instance.journal.Sync(); nil != syncErr {
        return syncErr
    }

    instance.journalSize = int64(len(header))
    instance.journalHeaderSize = instance.journalSize

    return nil
}

/* the record is already durable, so a failed compaction is logged rather than failing the write */
func (instance *FileStorage) compactPastThresholdLocked() {
    if instance.journalSize <= int64(instance.journalMaxBytes.Value) {
        return
    }

    if compactErr := instance.compactLocked(); nil != compactErr && nil != instance.logger {
        instance.logger.Warning(
            "session storage journal compaction failed",
            loggingcontract.Context{
                "path":         instance.path,
                "journalBytes": instance.journalSize,
                "error":        compactErr.Error(),
            },
        )
    }
}

/* lapsed sessions leave memory without a record: the replay drops them by their expiry */
func (instance *FileStorage) purgeLapsedInMemoryLocked() {
    if instance.clock.Now().UnixNano() < instance.earliestExpiry {
        return
    }

    instance.purgeExpiredLocked()
    instance.earliestExpiry = instance.earliestExpiryLocked()
}

func (instance *FileStorage) reachJournalPoint(point journalPoint) {
    if nil != instance.journalSeam {
        instance.journalSeam(point)
    }
}

func reportSessionJournalReplay(logger loggingcontract.Logger, path string, replay sessionJournalReplay) {
    if nil == logger {
        return
    }

    context := loggingcontract.Context{
        "path":     sessionJournalPath(path),
        "replayed": replay.replayed,
    }

    if true == replay.stale {
        context["discarded"] = replay.discarded
        logger.Warning("session storage journal does not extend the snapshot on disk, discarded", context)

        return
    }

    if true == replay.tailCut {
        logger.Warning("session storage journal ended in a torn record, cut at the last whole one", context)

        return
    }

    if 0 < replay.replayed {
        logger.Info("session storage journal replayed into the snapshot", context)
    }
}
