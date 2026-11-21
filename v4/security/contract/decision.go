package contract

/* DecisionStrategyUnspecified is the zero value and is refused by the framework's decision manager, so a strategy left out is never read as the most permissive one. */
const (
    DecisionStrategyUnspecified DecisionStrategy = iota
    DecisionStrategyAffirmative
    DecisionStrategyConsensus
    DecisionStrategyUnanimous
)

type DecisionStrategy int
