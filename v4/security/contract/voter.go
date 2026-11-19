package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type VoteResult int

const (
    VoteAbstain VoteResult = iota
    VoteDenied
    VoteGranted
)

type Voter interface {
    Supports(attribute string, subject any) bool

    /* Vote decides through the runtime it is handed, the request's, reading stores and services through it. An error ends the attribute's decision, since a missing vote cannot be counted as an abstention; it is answered 503 unless it answers a 4xx, the voter's own refusal, and DecideAny still grants on another attribute; the result beside an error is not read. */
    Vote(runtimeInstance containercontract.Runtime, token Token, attribute string, subject any) (VoteResult, error)
}
