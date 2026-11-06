package exception

import (
    "reflect"
)

/* chainLinkLimit bounds a search whose links carry no pointer identity to recognise a cycle by, and a chain that grows a new link on every Unwrap. */
const chainLinkLimit = 65536

var chainErrorType = reflect.TypeOf((*error)(nil)).Elem()

/* ChainAs answers what errors.As answers and returns on a chain whose Unwrap reaches itself, on which errors.As never returns. A link met twice is read once, and a search stops after 65,536 links. It panics on a target errors.As panics on. */
func ChainAs(err error, target any) bool {
    if nil == err {
        return false
    }

    targetPointer := reflect.ValueOf(target)
    if reflect.Pointer != targetPointer.Kind() || true == targetPointer.IsNil() {
        panic("exception: ChainAs target must be a non-nil pointer")
    }

    targetValue := targetPointer.Elem()
    targetType := targetValue.Type()
    if reflect.Interface != targetType.Kind() && false == targetType.Implements(chainErrorType) {
        panic("exception: ChainAs target must be a pointer to an interface or to a type implementing error")
    }

    return chainSearch(err, func(link error) bool {
        if true == reflect.TypeOf(link).AssignableTo(targetType) {
            targetValue.Set(reflect.ValueOf(link))

            return true
        }

        matcher, isMatcher := link.(interface{ As(any) bool })

        return true == isMatcher && true == matcher.As(target)
    })
}

/* ChainIs answers what errors.Is answers and returns on a chain whose Unwrap reaches itself, on which errors.Is never returns. A link met twice is read once, and a search stops after 65,536 links. */
func ChainIs(err error, target error) bool {
    if nil == target {
        return nil == err
    }

    return chainSearch(err, func(link error) bool {
        if true == reflect.TypeOf(link).Comparable() && link == target {
            return true
        }

        matcher, isMatcher := link.(interface{ Is(error) bool })

        return true == isMatcher && true == matcher.Is(target)
    })
}

/* chainSearch walks depth first, in the order errors.As and errors.Is read a chain; a link that panics while it is read panics through the search, as it does through errors.As and errors.Is. */
func chainSearch(err error, matches func(link error) bool) bool {
    visited := newChainLinkSet()
    pending := []error{err}

    for walked := 0; 0 < len(pending) && walked < chainLinkLimit; {
        current := pending[len(pending)-1]
        pending = pending[:len(pending)-1]

        if nil == current || true == visited.seen(current) {
            continue
        }
        walked++

        if true == matches(current) {
            return true
        }

        switch wrapper := current.(type) {
        case interface{ Unwrap() error }:
            pending = append(pending, wrapper.Unwrap())
        case interface{ Unwrap() []error }:
            links := wrapper.Unwrap()
            for index := len(links) - 1; 0 <= index; index-- {
                pending = append(pending, links[index])
            }
        }
    }

    return false
}

/* chainLinkSet recognises the links of a chain a walk already met. It holds each link itself, not its address, so a link the walk has passed cannot be collected and its address handed to a new link. */
type chainLinkSet struct {
    links map[error]struct{}
}

func newChainLinkSet() *chainLinkSet {
    return &chainLinkSet{links: map[error]struct{}{}}
}

/* seen records a pointer link and answers whether it was met before; a value link has no identity and answers false. */
func (instance *chainLinkSet) seen(link error) bool {
    reflected := reflect.ValueOf(link)
    if reflect.Pointer != reflected.Kind() || true == reflected.IsNil() {
        return false
    }

    if _, seen := instance.links[link]; true == seen {
        return true
    }

    instance.links[link] = struct{}{}

    return false
}
