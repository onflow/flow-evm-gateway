package models_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/onflow/flow-go/module/irrecoverable"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_Stream(t *testing.T) {

	t.Run("unsubscribe before subscribing", func(t *testing.T) {
		p := newMockPublisher()
		s := newMockSubscription(func(mockData) error { return nil })

		require.NotPanics(t, func() {
			p.Unsubscribe(s)
		})
	})

	t.Run("subscribe, publish, unsubscribe, publish", func(t *testing.T) {
		p := newMockPublisher()

		ctx := context.Background()
		ictx, cancel := irrecoverable.NewMockSignalerContextWithCancel(t, ctx)
		defer cancel()

		p.Start(ictx)

		callbackChan := make(chan struct{})

		f := func(mockData) error {
			callbackChan <- struct{}{}
			return nil
		}

		s1 := newMockSubscription(f)
		s2 := newMockSubscription(f)

		p.Subscribe(s1)
		p.Subscribe(s2)

		p.Publish(mockData{})

		<-callbackChan
		<-callbackChan

		p.Unsubscribe(s1)

		p.Publish(mockData{})

		<-callbackChan
	})

	t.Run("concurrent subscribe, publish, unsubscribe, publish", func(t *testing.T) {
		ctx := context.Background()
		ictx, cancel := irrecoverable.NewMockSignalerContextWithCancel(t, ctx)
		defer cancel()

		p := newMockPublisher()
		p.Start(ictx)

		stopPublishing := make(chan struct{})

		published := make(chan struct{})

		// publishing
		go func() {
			for {
				select {
				case <-stopPublishing:
					return
				case <-time.After(time.Millisecond * 1):
					p.Publish(mockData{})

					select {
					case published <- struct{}{}:
					default:
					}
				}
			}
		}()

		waitAllSubscribed := sync.WaitGroup{}
		waitAllUnsubscribed := sync.WaitGroup{}

		// 10 goroutines adding 10 subscribers each
		// and then unsubscribe all
		waitAllSubscribed.Add(10)
		waitAllUnsubscribed.Add(10)
		for i := 0; i < 10; i++ {
			go func() {
				subscriptions := make([]*mockSubscription, 10)
				callCount := make([]atomic.Uint64, 10)

				for j := 0; j < 10; j++ {
					callCount[j].Store(0)

					s := newMockSubscription(
						func(data mockData) error {
							callCount[j].Add(1)
							return nil
						},
					)
					subscriptions[j] = s
					p.Subscribe(s)

				}
				waitAllSubscribed.Done()
				waitAllSubscribed.Wait()

				// wait for all subscribers to receive data
				for i := 0; i < 10; i++ {
					<-published
				}

				for _, s := range subscriptions {
					p.Unsubscribe(s)
				}

				// there should be at least 1 call
				for j := 0; j < 10; j++ {
					require.GreaterOrEqual(t, callCount[j].Load(), uint64(10))
				}

				waitAllUnsubscribed.Done()
			}()
		}

		waitAllUnsubscribed.Wait()
		close(stopPublishing)
	})

	t.Run("error handling", func(t *testing.T) {
		errContent := fmt.Errorf("failed to process data")
		gotError := make(chan struct{})

		ctx := context.Background()
		ictx := irrecoverable.NewMockSignalerContext(t, ctx)
		ictx.On("Throw", errContent).Run(func(args mock.Arguments) {
			close(gotError)
		})

		p := newMockPublisher()
		p.Start(ictx)

		s := newMockSubscription(func(data mockData) error {
			return errContent
		})

		p.Subscribe(s)

		p.Publish(mockData{})

		<-gotError
	})
}

type mockData struct{}

type mockSubscription struct {
	*models.Subscription[mockData]
}

func newMockSubscription(callback func(mockData) error) *mockSubscription {
	s := &mockSubscription{}
	s.Subscription = models.NewSubscription[mockData](callback)
	return s
}

func newMockPublisher() *models.Publisher[mockData] {
	return models.NewPublisher[mockData](zerolog.Nop())
}
