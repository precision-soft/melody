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
