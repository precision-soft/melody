package contract

type AccessDecisionManager interface {
	DecideAll(token Token, attributes []string, subject any) error

	DecideAny(token Token, attributes []string, subject any) error
}
