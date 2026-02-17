package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemember_ProtectAgainstStampede_ExecutesCallbackOnce(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		100,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheManager := NewManager(
		backend,
		NewJsonSerializer(),
	)

	releaseCallbackChannel := make(chan struct{})

	var callbackCalls int64
	callback := func(ctx context.Context) (any, error) {
		atomic.AddInt64(&callbackCalls, 1)
		<-releaseCallbackChannel
		return "value", nil
	}

	concurrency := 50

	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)

	errorChannel := make(chan error, concurrency)

	for index := 0; index < concurrency; index++ {
		go func() {
			defer waitGroup.Done()

			value, err := Remember(
				cacheManager,
				"product.1",
				time.Minute,
				callback,
				nil,
			)
			if nil != err {
				errorChannel <- err
				return
			}
			if "value" != value {
				errorChannel <- errors.New("unexpected value")
				return
			}
		}()
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	for {
		if 1 == atomic.LoadInt64(&callbackCalls) {
			break
		}

		select {
		case <-time.After(5 * time.Millisecond):
			continue
		case <-deadline.C:
			t.Fatalf("expected callback to be called once before release, got %d", atomic.LoadInt64(&callbackCalls))
		}
	}

	close(releaseCallbackChannel)
	waitGroup.Wait()
	close(errorChannel)

	for err := range errorChannel {
		if nil != err {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if 1 != atomic.LoadInt64(&callbackCalls) {
		t.Fatalf("expected callback to be called once, got %d", atomic.LoadInt64(&callbackCalls))
	}
}

func TestRemember_StampedeProtectionDisabled_AllowsParallelCallbackCalls(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		100,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheManager := NewManager(
		backend,
		NewJsonSerializer(),
	)

	releaseCallbackChannel := make(chan struct{})

	var callbackCalls int64
	callback := func(ctx context.Context) (any, error) {
		atomic.AddInt64(&callbackCalls, 1)
		<-releaseCallbackChannel
		return "value", nil
	}

	concurrency := 50

	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)

	option := NewDefaultRememberOption().
		WithStampedeProtectionEnabled(false).
		WithWaitTimeout(-1).
		WithCancelable(false)

	for index := 0; index < concurrency; index++ {
		go func() {
			defer waitGroup.Done()

			_, _ = Remember(
				cacheManager,
				"product.2",
				time.Minute,
				callback,
				option,
			)
		}()
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	for {
		if 2 <= atomic.LoadInt64(&callbackCalls) {
			break
		}

		select {
		case <-time.After(5 * time.Millisecond):
			continue
		case <-deadline.C:
			t.Fatalf("expected callback to be called at least twice before release, got %d", atomic.LoadInt64(&callbackCalls))
		}
	}

	close(releaseCallbackChannel)
	waitGroup.Wait()

	if 2 > atomic.LoadInt64(&callbackCalls) {
		t.Fatalf("expected callback to be called at least twice, got %d", atomic.LoadInt64(&callbackCalls))
	}
}

func TestRemember_WaitTimeoutIsPerCaller_DoesNotPoisonInFlightCall(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		100,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheManager := NewManager(
		backend,
		NewJsonSerializer(),
	)

	releaseCallbackChannel := make(chan struct{})

	var callbackCalls int64
	callback := func(ctx context.Context) (any, error) {
		atomic.AddInt64(&callbackCalls, 1)
		<-releaseCallbackChannel
		return "value", nil
	}

	startTime := time.Now()

	resultChannelA := make(chan error, 1)
	go func() {
		_, err := Remember(
			cacheManager,
			"product.3",
			time.Minute,
			callback,
			NewDefaultRememberOption().
				WithWaitTimeout(-1).
				WithCancelable(false),
		)
		resultChannelA <- err
	}()

	time.Sleep(10 * time.Millisecond)

	resultChannelB := make(chan error, 1)
	go func() {
		_, err := Remember(
			cacheManager,
			"product.3",
			time.Minute,
			callback,
			NewDefaultRememberOption().
				WithWaitTimeout(100*time.Millisecond).
				WithCancelable(false),
		)
		resultChannelB <- err
	}()

	time.Sleep(20 * time.Millisecond)

	resultChannelC := make(chan any, 1)
	resultChannelCErr := make(chan error, 1)
	go func() {
		value, err := Remember(
			cacheManager,
			"product.3",
			time.Minute,
			callback,
			NewDefaultRememberOption().
				WithWaitTimeout(150*time.Millisecond).
				WithCancelable(false),
		)
		resultChannelC <- value
		resultChannelCErr <- err
	}()

	elapsedUntilRelease := time.Since(startTime)
	if 120*time.Millisecond > elapsedUntilRelease {
		time.Sleep(120*time.Millisecond - elapsedUntilRelease)
	}

	close(releaseCallbackChannel)

	errA := <-resultChannelA
	if nil != errA {
		t.Fatalf("unexpected error for caller A: %v", errA)
	}

	errB := <-resultChannelB
	if nil == errB {
		t.Fatalf("expected timeout error for caller B")
	}

	errC := <-resultChannelCErr
	if nil != errC {
		t.Fatalf("unexpected error for caller C: %v", errC)
	}

	valueC := <-resultChannelC
	if "value" != valueC {
		t.Fatalf("unexpected value for caller C")
	}

	if 1 != atomic.LoadInt64(&callbackCalls) {
		t.Fatalf("expected callback to be called once, got %d", atomic.LoadInt64(&callbackCalls))
	}
}

func TestRemember_CancelableGroupIsSeparatedFromNonCancelableGroup(t *testing.T) {
	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

	backend := NewInMemoryBackend(
		100,
		time.Hour,
		clockInstance,
	)
	defer backend.Close()

	cacheManager := NewManager(
		backend,
		NewJsonSerializer(),
	)

	releaseCallbackNonCancelableChannel := make(chan struct{})
	releaseCallbackCancelableChannel := make(chan struct{})

	var nonCancelableCalls int64
	var cancelableCalls int64

	nonCancelableCallback := func(ctx context.Context) (any, error) {
		atomic.AddInt64(&nonCancelableCalls, 1)
		<-releaseCallbackNonCancelableChannel
		return "nonCancelable", nil
	}

	cancelableCallback := func(ctx context.Context) (any, error) {
		atomic.AddInt64(&cancelableCalls, 1)
		<-releaseCallbackCancelableChannel
		return "cancelable", nil
	}

	resultChannelNonCancelable := make(chan any, 1)
	resultChannelCancelable := make(chan any, 1)

	go func() {
		value, _ := Remember(
			cacheManager,
			"product.4",
			time.Minute,
			nonCancelableCallback,
			NewDefaultRememberOption().
				WithWaitTimeout(-1).
				WithCancelable(false),
		)
		resultChannelNonCancelable <- value
	}()

	go func() {
		value, _ := Remember(
			cacheManager,
			"product.4",
			time.Minute,
			cancelableCallback,
			NewDefaultRememberOption().
				WithWaitTimeout(-1).
				WithCancelable(true),
		)
		resultChannelCancelable <- value
	}()

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	for {
		if 1 == atomic.LoadInt64(&nonCancelableCalls) && 1 == atomic.LoadInt64(&cancelableCalls) {
			break
		}

		select {
		case <-time.After(5 * time.Millisecond):
			continue
		case <-deadline.C:
			t.Fatalf("expected both callbacks to be called once, got nonCancelable=%d cancelable=%d", atomic.LoadInt64(&nonCancelableCalls), atomic.LoadInt64(&cancelableCalls))
		}
	}

	close(releaseCallbackNonCancelableChannel)
	close(releaseCallbackCancelableChannel)

	valueNonCancelable := <-resultChannelNonCancelable
	valueCancelable := <-resultChannelCancelable

	if "nonCancelable" != valueNonCancelable {
		t.Fatalf("unexpected nonCancelable value")
	}
	if "cancelable" != valueCancelable {
		t.Fatalf("unexpected cancelable value")
	}
}
