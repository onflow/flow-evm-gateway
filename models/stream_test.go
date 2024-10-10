package models_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onflow/flow-evm-gateway/models"
	"github.com/stretchr/testify/require"
)

func Test_Stream(t *testing.T) {

	t.Run("unsubscribe before subscribing", func(t *testing.T) {
		p := newMockPublisher()
		s := newMockSubscription()

		require.NotPanics(t, func() {
			p.Unsubscribe(s)
		})
	})

	t.Run("subscribe, publish, unsubscribe, publish", func(t *testing.T) {
		p := newMockPublisher()
		s1 := newMockSubscription()
		s2 := newMockSubscription()

		p.Subscribe(s1)
		p.Subscribe(s2)

		p.Publish(mockData{})

		require.Equal(t, uint64(1), s1.CallCount())
		require.Equal(t, uint64(1), s2.CallCount())

		p.Unsubscribe(s1)

		p.Publish(mockData{})

		require.Equal(t, uint64(1), s1.CallCount())
		require.Equal(t, uint64(2), s2.CallCount())
	})

	t.Run("concurrent subscribe, publish, unsubscribe, publish", func(t *testing.T) {

		p := newMockPublisher()

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

				for j := 0; j < 10; j++ {
					s := newMockSubscription()
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
					require.GreaterOrEqual(t, subscriptions[j].CallCount(), uint64(10))
				}

				waitAllUnsubscribed.Done()
			}()
		}

		waitAllUnsubscribed.Wait()
		close(stopPublishing)
	})

	t.Run("error handling", func(t *testing.T) {
		p := newMockPublisher()
		s := &mockSubscription{}
		errContent := fmt.Errorf("failed to process data")

		s.Subscription = models.NewSubscription[mockData](func(data mockData) error {
			s.callCount.Add(1)
			return errContent
		})

		p.Subscribe(s)

		shouldReceiveError := make(chan struct{})
		ready := make(chan struct{})
		go func() {
			close(ready)
			select {
			case err := <-s.Error():
				require.ErrorIs(t, err, errContent)
			case <-shouldReceiveError:
				require.Fail(t, "should have received error")
			}
		}()
		<-ready

		p.Publish(mockData{})
		close(shouldReceiveError)
	})
}

type mockData struct{}

type mockSubscription struct {
	*models.Subscription[mockData]
	callCount atomic.Uint64
}

func newMockSubscription() *mockSubscription {
	s := &mockSubscription{}
	s.Subscription = models.NewSubscription[mockData](func(data mockData) error {
		s.callCount.Add(1)
		return nil
	})
	return s
}

func (s *mockSubscription) CallCount() uint64 {
	return s.callCount.Load()
}

func newMockPublisher() *models.Publisher[mockData] {
	return models.NewPublisher[mockData]()
}
