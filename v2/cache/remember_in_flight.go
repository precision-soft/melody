package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/precision-soft/melody/v2/exception"
)

const rememberInFlightShardCount = 64

type rememberInFlightShard struct {
	mutex         sync.Mutex
	inFlightByKey map[string]*rememberInFlightCall
}

var rememberInFlightShardList = buildRememberInFlightShardList()

func newRememberInFlightCall(isCancelable bool) *rememberInFlightCall {
	contextInstance := context.Background()
	var cancelFunc context.CancelFunc = nil

	if true == isCancelable {
		derivedContext, derivedCancelFunc := context.WithCancel(context.Background())
		contextInstance = derivedContext
		cancelFunc = derivedCancelFunc
	}

	return &rememberInFlightCall{
		done:         make(chan struct{}),
		context:      contextInstance,
		cancelFunc:   cancelFunc,
		isCancelable: isCancelable,
	}
}

type rememberInFlightCall struct {
	doneOnce sync.Once
	done     chan struct{}
	result   any
	err      error

	waitersCount int64

	context      context.Context
	cancelOnce   sync.Once
	cancelFunc   context.CancelFunc
	isCancelable bool
}

func (instance *rememberInFlightCall) AddWaiter() {
	atomic.AddInt64(&instance.waitersCount, 1)
}

func (instance *rememberInFlightCall) RemoveWaiter() {
	remainingWaiters := atomic.AddInt64(&instance.waitersCount, -1)
	if 0 != remainingWaiters {
		return
	}

	if false == instance.isCancelable {
		return
	}

	if nil == instance.cancelFunc {
		return
	}

	instance.cancelOnce.Do(
		func() {
			instance.cancelFunc()
		},
	)
}

func (instance *rememberInFlightCall) Context() context.Context {
	return instance.context
}

func (instance *rememberInFlightCall) Wait(waitTimeout time.Duration, key string) (any, error) {
	if 0 == waitTimeout {
		return nil, exception.NewError(
			"cache remember callback timed out",
			map[string]any{
				"key":     key,
				"timeout": waitTimeout.String(),
			},
			nil,
		)
	}

	if 0 > waitTimeout {
		<-instance.done
		return instance.result, instance.err
	}

	timer := time.NewTimer(waitTimeout)
	defer timer.Stop()

	select {
	case <-instance.done:
		return instance.result, instance.err
	case <-timer.C:
		return nil, exception.NewError(
			"cache remember callback timed out",
			map[string]any{
				"key":     key,
				"timeout": waitTimeout.String(),
			},
			nil,
		)
	}
}

func (instance *rememberInFlightCall) Complete(result any, err error) {
	instance.doneOnce.Do(
		func() {
			instance.result = result
			instance.err = err
			close(instance.done)
		},
	)
}
