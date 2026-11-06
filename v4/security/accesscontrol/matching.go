package accesscontrol

/* Matching selects how a rule's path is compared against a request path, from the narrowest reach to the widest: MatchingExact governs one spelling, MatchingSegmentPrefix the path and its descendants under a "/" boundary, MatchingRawPrefix every path that begins with the spelling ("/admin" claims "/administrator"), and MatchingRegex every path the unanchored pattern matches anywhere. The zero value is refused at construction, so a caller never inherits a reach they did not choose. */
type Matching int

const (
    /* MatchingUnspecified is the zero value and is refused: NewRule requires the mode to be named. */
    MatchingUnspecified Matching = iota

    /* MatchingExact governs the path itself and nothing beneath it. */
    MatchingExact

    /* MatchingSegmentPrefix governs the path and its descendants under a "/" boundary. */
    MatchingSegmentPrefix

    /* MatchingRawPrefix governs every path that begins with the spelling, across segment boundaries. */
    MatchingRawPrefix

    /* MatchingRegex governs every path the pattern matches anywhere, the pattern being compiled unanchored. */
    MatchingRegex
)

/* String answers the name a refusal reports, so a caller reading the error sees the mode rather than an integer. */
func (instance Matching) String() string {
    switch instance {
    case MatchingExact:
        return "exact"
    case MatchingSegmentPrefix:
        return "segment prefix"
    case MatchingRawPrefix:
        return "raw prefix"
    case MatchingRegex:
        return "regex"
    }

    return "unspecified"
}
