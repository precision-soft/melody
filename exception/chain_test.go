package exception

import (
    "errors"
    "fmt"
    "runtime"
    "testing"
    "time"
)

type chainCycleLink struct {
    next error
}

func (instance *chainCycleLink) Error() string {
    return "cyclic link"
}

func (instance *chainCycleLink) Unwrap() error {
    return instance.next
}

func newCycle() *chainCycleLink {
    first := &chainCycleLink{}
    first.next = &chainCycleLink{next: first}

    return first
}

type selfUnwrappingValue struct{}

func (instance selfUnwrappingValue) Error() string {
    return "self unwrapping value"
}

func (instance selfUnwrappingValue) Unwrap() error {
    return instance
}

type targetLink struct {
    name string
}

func (instance *targetLink) Error() string {
    return instance.name
}

type asLink struct {
    answer *targetLink
}

func (instance *asLink) Error() string {
    return "as link"
}

func (instance *asLink) As(target any) bool {
    typedTarget, isTarget := target.(**targetLink)
    if false == isTarget {
        return false
    }

    *typedTarget = instance.answer

    return true
}

type isLink struct {
    target error
}

func (instance *isLink) Error() string {
    return "is link"
}

func (instance *isLink) Is(target error) bool {
    return instance.target == target
}

type panickingUnwrapLink struct{}

func (instance *panickingUnwrapLink) Error() string {
    return "panicking unwrap"
}

func (instance *panickingUnwrapLink) Unwrap() error {
    panic("unwrap panicked")
}

type incomparableLink struct {
    parts []string
}

func (instance incomparableLink) Error() string {
    return "incomparable link"
}

func returnsInTime(t *testing.T, callback func() bool) bool {
    t.Helper()

    done := make(chan bool, 1)
    go func() {
        done <- callback()
    }()

    select {
    case found := <-done:
        return found
    case <-time.After(5 * time.Second):
        t.Fatal("the search must return on a chain that reaches itself")

        return false
    }
}

func TestChainSearch_ReturnsOnAChainThatReachesItself(t *testing.T) {
    var absent *targetLink
    if true == returnsInTime(t, func() bool { return ChainAs(newCycle(), &absent) }) {
        t.Fatal("expected As to find nothing of a type the cycle does not hold")
    }

    if true == returnsInTime(t, func() bool { return ChainIs(newCycle(), errors.New("absent")) }) {
        t.Fatal("expected Is to find nothing the cycle does not hold")
    }
}

func TestChainSearch_FindsALinkOnAChainThatReachesItself(t *testing.T) {
    target := &targetLink{name: "target"}
    cycle := newCycle()
    cycle.next.(*chainCycleLink).next = errors.Join(cycle, target)

    var found *targetLink
    if false == returnsInTime(t, func() bool { return ChainAs(cycle, &found) }) || target != found {
        t.Fatalf("expected As to reach the link behind the cycle, got %v", found)
    }

    if false == returnsInTime(t, func() bool { return ChainIs(cycle, target) }) {
        t.Fatal("expected Is to reach the link behind the cycle")
    }
}

func TestChainSearch_BoundsAChainWithoutPointerIdentity(t *testing.T) {
    var absent *targetLink
    if true == returnsInTime(t, func() bool { return ChainAs(selfUnwrappingValue{}, &absent) }) {
        t.Fatal("expected As to find nothing of a type the chain does not hold")
    }

    if true == returnsInTime(t, func() bool { return ChainIs(selfUnwrappingValue{}, errors.New("absent")) }) {
        t.Fatal("expected Is to find nothing the chain does not hold")
    }
}

type chainPlainLink struct {
    next error
}

func (instance *chainPlainLink) Error() string {
    return "plain link"
}

func (instance *chainPlainLink) Unwrap() error {
    return instance.next
}

func TestChainSearch_TheLinkLimitIsTheBound(t *testing.T) {
    target := &targetLink{name: "target"}

    chainOf := func(wrappers int) error {
        var chain error = target
        for index := 0; index < wrappers; index++ {
            chain = &chainPlainLink{next: chain}
        }

        return chain
    }

    var found *targetLink
    if false == ChainAs(chainOf(chainLinkLimit-1), &found) {
        t.Fatal("expected ChainAs to find the link at the limit")
    }

    if false == ChainIs(chainOf(chainLinkLimit-1), target) {
        t.Fatal("expected ChainIs to find the link at the limit")
    }

    found = nil
    if true == ChainAs(chainOf(chainLinkLimit), &found) {
        t.Fatal("expected ChainAs to stop at the limit")
    }

    if true == ChainIs(chainOf(chainLinkLimit), target) {
        t.Fatal("expected ChainIs to stop at the limit")
    }
}

func TestChainSearch_ABranchOfAWideJoinIsReachedPastAThousandSiblings(t *testing.T) {
    target := &targetLink{name: "target"}

    links := make([]error, 0, 2001)
    for index := 0; index < 2000; index++ {
        links = append(links, &targetLink{name: "sibling"})
    }
    links = append(links, target)

    if false == ChainIs(errors.Join(links...), target) {
        t.Fatal("expected the link behind the siblings")
    }
}

func TestChainAs_PanicsOnATargetTheStandardLibraryPanicsOn(t *testing.T) {
    invalidString := "not an error"
    var nilPointer *error

    for name, target := range map[string]any{
        "nil":                  nil,
        "not a pointer":        "text",
        "a nil pointer":        nilPointer,
        "a pointer to a value": &invalidString,
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                if nil == recover() {
                    t.Fatal("expected a panic")
                }
            }()

            ChainAs(errors.New("any"), target)
        })
    }
}

func TestChainAs_ANilErrorHoldsNothingWhateverTheTarget(t *testing.T) {
    if true == ChainAs(nil, nil) {
        t.Fatal("expected a nil error to hold nothing")
    }
}

func TestChainSearch_AnswersWhatTheStandardLibraryAnswersOnAChainItCanWalk(t *testing.T) {
    target := &targetLink{name: "target"}
    other := &targetLink{name: "other"}
    answered := &targetLink{name: "answered"}
    sentinel := errors.New("sentinel")

    chains := map[string]error{
        "nil":                 nil,
        "the target":          target,
        "a typed nil":         (*targetLink)(nil),
        "a wrap":              fmt.Errorf("wrapped: %w", target),
        "a join":              errors.Join(sentinel, target),
        "two matches":         errors.Join(fmt.Errorf("first: %w", other), target),
        "a deep first branch": errors.Join(fmt.Errorf("a: %w", fmt.Errorf("b: %w", other)), target),
        "a nil in a join":     fmt.Errorf("two: %w %w", sentinel, target),
        "an As method":        fmt.Errorf("wrapped: %w", &asLink{answer: answered}),
        "an Is method":        fmt.Errorf("wrapped: %w", &isLink{target: sentinel}),
        "the sentinel":        fmt.Errorf("wrapped: %w", sentinel),
        "an incomparable":     fmt.Errorf("wrapped: %w", incomparableLink{parts: []string{"a"}}),
        "no match":            errors.New("no match"),
    }

    for name, chain := range chains {
        t.Run(name, func(t *testing.T) {
            var standardFound *targetLink
            var chainFound *targetLink

            standardAnswer := errors.As(chain, &standardFound)
            chainAnswer := ChainAs(chain, &chainFound)
            if standardAnswer != chainAnswer || standardFound != chainFound {
                t.Fatalf("As: the standard library answered %v %v, the search %v %v", standardAnswer, standardFound, chainAnswer, chainFound)
            }

            for _, isTarget := range []error{target, other, sentinel, incomparableLink{parts: []string{"a"}}} {
                if errors.Is(chain, isTarget) != ChainIs(chain, isTarget) {
                    t.Fatalf("Is: the answers differ for the target %v", isTarget)
                }
            }

            if errors.Is(chain, nil) != ChainIs(chain, nil) {
                t.Fatal("Is: the answers differ for a nil target")
            }
        })
    }
}

func TestChainSearch_ALinkThatPanicsWhileItIsReadPanicsThroughTheSearch(t *testing.T) {
    assertPanics := func(name string, callback func()) {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected %s to panic as the standard library does", name)
            }
        }()

        callback()
    }

    var absent *targetLink
    assertPanics("As", func() { ChainAs(&panickingUnwrapLink{}, &absent) })
    assertPanics("Is", func() { ChainIs(&panickingUnwrapLink{}, errors.New("absent")) })
}

func TestChainLinkSet_RecognisesAPointerLinkAndNeverAValueLink(t *testing.T) {
    links := newChainLinkSet()
    link := &chainCycleLink{}

    if true == links.seen(link) {
        t.Fatal("expected a first meeting")
    }

    if false == links.seen(link) {
        t.Fatal("expected the second meeting to be recognised")
    }

    if true == links.seen(&chainCycleLink{}) {
        t.Fatal("expected another pointer to be a first meeting")
    }

    if true == links.seen(selfUnwrappingValue{}) || true == links.seen(selfUnwrappingValue{}) {
        t.Fatal("expected a value link to have no identity")
    }

    if true == links.seen((*chainCycleLink)(nil)) || true == links.seen((*chainCycleLink)(nil)) {
        t.Fatal("expected a typed-nil link to have no identity")
    }
}

type outerLink struct {
    inner innerLink
}

func (instance *outerLink) Error() string {
    return "outer link"
}

type innerLink struct {
    name string
}

func (instance *innerLink) Error() string {
    return "inner link"
}

func TestChainLinkSet_TellsApartTwoTypesAtOneAddress(t *testing.T) {
    links := newChainLinkSet()
    outer := &outerLink{}

    if true == links.seen(outer) || true == links.seen(&outer.inner) {
        t.Fatal("expected a link of another type at the same address to be a first meeting")
    }
}

type incomparableTargetMatcher struct{}

func (instance *incomparableTargetMatcher) Error() string {
    return "incomparable target matcher"
}

func (instance *incomparableTargetMatcher) Is(target error) bool {
    _, isIncomparable := target.(incomparableLink)

    return isIncomparable
}

func TestChainIs_AnIncomparableLinkDoesNotEndTheSearch(t *testing.T) {
    chain := errors.Join(incomparableLink{parts: []string{"a"}}, &incomparableTargetMatcher{})
    target := incomparableLink{parts: []string{"b"}}

    if false == errors.Is(chain, target) || false == ChainIs(chain, target) {
        t.Fatal("expected the search to go past the incomparable link, as the standard library does")
    }
}

type chainGeneratedLink struct {
    remaining int
    target    error
    padding   [128]byte
}

func (instance *chainGeneratedLink) Error() string {
    return "generated link"
}

func (instance *chainGeneratedLink) Unwrap() error {
    runtime.GC()

    if 0 == instance.remaining {
        return instance.target
    }

    return &chainGeneratedLink{remaining: instance.remaining - 1, target: instance.target}
}

func TestChainSearch_AGeneratedChainIsNotCutByAnAddressTheCollectorReused(t *testing.T) {
    target := errors.New("target")

    if false == ChainIs(&chainGeneratedLink{remaining: 100, target: target}, target) {
        t.Fatal("expected the search to reach the end of a finite chain")
    }
}
