package security

func NewMatchedAccessControlRule(
    pathPrefix string,
    attributes []string,
    source Source,
    ruleIndex int,
    firewall string,
) *MatchedAccessControlRule {
    return &MatchedAccessControlRule{
        pathPrefix: pathPrefix,
        attributes: append([]string{}, attributes...),
        source:     source,
        ruleIndex:  ruleIndex,
        firewall:   firewall,
    }
}

type MatchedAccessControlRule struct {
    pathPrefix string
    attributes []string
    source     Source
    ruleIndex  int
    firewall   string
}

func (instance *MatchedAccessControlRule) PathPrefix() string {
    return instance.pathPrefix
}

func (instance *MatchedAccessControlRule) Attributes() []string {
    return append([]string{}, instance.attributes...)
}

func (instance *MatchedAccessControlRule) Source() Source {
    return instance.source
}

func (instance *MatchedAccessControlRule) RuleIndex() int {
    return instance.ruleIndex
}

func (instance *MatchedAccessControlRule) Firewall() string {
    return instance.firewall
}
