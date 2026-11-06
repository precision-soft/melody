package exception

import (
    "reflect"
)

const chainLinkLimit = 65536

var chainErrorType = reflect.TypeOf((*error)(nil)).Elem()

/* ChainAs answers what errors.As answers and returns on a chain whose Unwrap reaches itself; it stops after 65,536 links. */
func ChainAs(err error, target any) bool {
    if nil == err {
        return false
    }

    targetPointer := reflect.ValueOf(target)
    if reflect.Pointer != targetPointer.Kind() || true == targetPointer.IsNil() {
        Panic(NewError("the target of a chain search must be a non-nil pointer", nil, nil))
    }

    targetValue := targetPointer.Elem()
    targetType := targetValue.Type()
    if reflect.Interface != targetType.Kind() && false == targetType.Implements(chainErrorType) {
        Panic(NewError("the target of a chain search must be a pointer to an interface or to a type implementing error", nil, nil))
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

/* ChainIs answers what errors.Is answers and returns on a chain whose Unwrap reaches itself; it stops after 65,536 links. */
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

/* holds each link, not its address, so a passed link cannot be collected and its address handed to a new link */
type chainLinkSet struct {
    links map[error]struct{}
}

func newChainLinkSet() *chainLinkSet {
    return &chainLinkSet{links: map[error]struct{}{}}
}

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
