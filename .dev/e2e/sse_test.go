package main

import (
    "strings"
    "testing"
    "time"
)

/* The frame parser is the riskiest new logic in the harness: every server-sent-event assertion reads its verdict out of it, and a parser that quietly mis-split a frame would turn the id-sanitisation and topic-isolation negatives into assertions that can never fail. These tests pin the grammar it implements. */

func TestParseServerSentEventFrame_ReadsEveryField(t *testing.T) {
    frame := parseServerSentEventFrame([]string{
        "id: event-7",
        "event: wire",
        "retry: 7000",
        "data: hello",
    })

    if "event-7" != frame.id {
        t.Fatalf("id parsed as %q, wanted %q", frame.id, "event-7")
    }
    if "wire" != frame.event {
        t.Fatalf("event parsed as %q, wanted %q", frame.event, "wire")
    }
    if "7000" != frame.retry {
        t.Fatalf("retry parsed as %q, wanted %q", frame.retry, "7000")
    }
    if "hello" != frame.data() {
        t.Fatalf("data parsed as %q, wanted %q", frame.data(), "hello")
    }
    if true == frame.isComment() {
        t.Fatal("a frame carrying fields must not be reported as a comment")
    }
}

/* the preamble assertion turns on this distinction: a comment-only frame and a frame that merely carries no data are different things on the wire, and treating the first as an event would let a stream that never flushed its preamble pass the ordering assertion. */
func TestParseServerSentEventFrame_CommentIsNotAField(t *testing.T) {
    frame := parseServerSentEventFrame([]string{": connected"})

    if false == frame.isComment() {
        t.Fatal("a line beginning with a colon is a comment")
    }
    if 1 != len(frame.commentList) || "connected" != frame.commentList[0] {
        t.Fatalf("comment parsed as %v, wanted [connected]", frame.commentList)
    }
    if "" != frame.data() {
        t.Fatalf("a comment frame carries no data, got %q", frame.data())
    }
}

/* a frame that carries a comment AND fields is an event, not a comment: the keepalive comment may share a frame with a real event, and stepping over it as if the whole frame were a comment would drop the event. */
func TestParseServerSentEventFrame_CommentBesideFieldsIsStillAnEvent(t *testing.T) {
    frame := parseServerSentEventFrame([]string{": keepalive", "data: real"})

    if true == frame.isComment() {
        t.Fatal("a frame carrying a data field is an event even when it also carries a comment")
    }
    if "real" != frame.data() {
        t.Fatalf("data parsed as %q, wanted %q", frame.data(), "real")
    }
}

/* the writer emits one data: line per line of the payload, so a multi-line payload has to be rejoined — a parser that kept only the last line would silently truncate every event carrying a newline. */
func TestParseServerSentEventFrame_JoinsMultipleDataLines(t *testing.T) {
    frame := parseServerSentEventFrame([]string{"data: first", "data: second", "data: third"})

    if "first\nsecond\nthird" != frame.data() {
        t.Fatalf("multi-line data joined as %q", frame.data())
    }
}

/* only ONE leading space is stripped after the colon, and only the FIRST colon separates the name from the value: a payload that is itself json ("data: {\"a\":1}") must survive intact, and so must a deliberate leading space. */
func TestSplitServerSentEventLine_KeepsTheValueIntact(t *testing.T) {
    for line, expected := range map[string]string{
        `data: {"topic":"x","count":1}`: `{"topic":"x","count":1}`,
        "data:  two spaces":             " two spaces",
        "data:no space at all":          "no space at all",
    } {
        name, value := splitServerSentEventLine(line)
        if "data" != name {
            t.Fatalf("%q parsed its name as %q, wanted data", line, name)
        }
        if expected != value {
            t.Fatalf("%q parsed its value as %q, wanted %q", line, value, expected)
        }
    }
}

/* a line with no colon is a field with an empty value, never a comment: reporting it as a comment would make a malformed frame look like a keepalive and be silently stepped over. */
func TestSplitServerSentEventLine_NoColonIsAFieldWithNoValue(t *testing.T) {
    name, value := splitServerSentEventLine("data")

    if "data" != name || "" != value {
        t.Fatalf("a colonless line parsed as name=%q value=%q, wanted name=data value=\"\"", name, value)
    }
}

/* the blank line is the frame terminator, so two events in one stream must come out as two frames — this is the exact property the id-sanitisation negative asserts the ABSENCE of, and a reader that merged them would make that assertion unfailable. */
func TestServerSentEventStreamReader_SplitsFramesOnTheBlankLine(t *testing.T) {
    stream := ": connected\n\nid: one\ndata: first\n\ndata: second\n\n"

    reader := newServerSentEventStreamReader(strings.NewReader(stream))

    preamble, arrived := reader.next(time.Second)
    if false == arrived || false == preamble.isComment() {
        t.Fatalf("wanted the preamble comment first, got arrived=%v %s", arrived, preamble.describe())
    }

    first, arrived := reader.next(time.Second)
    if false == arrived || "first" != first.data() || "one" != first.id {
        t.Fatalf("wanted the first event, got arrived=%v %s", arrived, first.describe())
    }

    second, arrived := reader.next(time.Second)
    if false == arrived || "second" != second.data() {
        t.Fatalf("wanted the second event, got arrived=%v %s", arrived, second.describe())
    }

    if _, extra := reader.next(200 * time.Millisecond); true == extra {
        t.Fatal("the stream held exactly three frames")
    }
}

/* CRLF line endings must parse identically: a proxy or a client library that normalises them would otherwise leave a trailing carriage return inside every value and every equality assertion in the section would fail. */
func TestServerSentEventStreamReader_HandlesCarriageReturns(t *testing.T) {
    reader := newServerSentEventStreamReader(strings.NewReader("data: value\r\n\r\n"))

    frame, arrived := reader.next(time.Second)
    if false == arrived {
        t.Fatal("no frame arrived from a CRLF stream")
    }
    if "value" != frame.data() {
        t.Fatalf("data parsed as %q, wanted %q", frame.data(), "value")
    }
}

/* a closed body must report "no frame" rather than block until the budget expires: the assertions distinguish a frame that did not arrive from a stream that ended, and only the second one means the application closed on us. */
func TestServerSentEventStreamReader_ReportsNoFrameWhenTheStreamEnds(t *testing.T) {
    reader := newServerSentEventStreamReader(strings.NewReader(""))

    if _, arrived := reader.next(time.Second); true == arrived {
        t.Fatal("an empty stream yields no frame")
    }
}
