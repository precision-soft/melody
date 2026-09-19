package accesscontrol

/* Matching selects how a rule's path is compared against a request path. Each value answers a different question, and the four are ordered here from the narrowest to the widest reach:

MatchingExact governs one spelling and nothing under it. MatchingSegmentPrefix governs the path and its descendants under a "/" boundary, so "/admin" claims "/admin" and "/admin/panel" but not "/administrator". MatchingRawPrefix governs every path that BEGINS with the spelling, so "/admin" claims "/administrator" and "/admin-tools" as readily as "/admin/panel". MatchingRegex governs every path the pattern matches ANYWHERE, since the pattern is compiled unanchored — "/admin" claims "/x/admin/y" too.

The zero value is refused at construction. The mode is the one decision that cannot be inferred from the path, and a default would let a caller inherit a reach they never chose. */
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
