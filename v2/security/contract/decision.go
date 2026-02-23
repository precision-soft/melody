package contract

const (
    DecisionStrategyAffirmative DecisionStrategy = iota
    DecisionStrategyConsensus
    DecisionStrategyUnanimous
)

type DecisionStrategy int
