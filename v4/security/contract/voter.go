package contract

type VoteResult int

const (
    VoteAbstain VoteResult = iota
    VoteDenied
    VoteGranted
)

type Voter interface {
    Supports(attribute string, subject any) bool

    Vote(token Token, attribute string, subject any) VoteResult
}

/* FallibleVoter is a Voter whose decision reads a store: the access decision manager calls VoteWithError instead of Vote, and an error ends the attribute's decision, since a missing vote cannot be counted as an abstention; it is answered 503 unless it answers a 4xx, the voter's own refusal, and DecideAny still grants on another attribute */
type FallibleVoter interface {
    Voter

    VoteWithError(token Token, attribute string, subject any) (VoteResult, error)
}
