package models

import (
	"sync"

	"github.com/onflow/flow-go/module/component"
	"github.com/onflow/flow-go/module/irrecoverable"
	"github.com/rs/zerolog"
)

type Publisher[T any] struct {
	component.Component
	cm *component.ComponentManager

	log zerolog.Logger

	mux         sync.RWMutex
	subscribers map[Subscriber[T]]struct{}

	publishChan chan T

	publisherExited chan struct{}
}

func NewPublisher[T any](log zerolog.Logger) *Publisher[T] {
	p := &Publisher[T]{
		mux:             sync.RWMutex{},
		log:             log,
		subscribers:     make(map[Subscriber[T]]struct{}),
		publishChan:     make(chan T),
		publisherExited: make(chan struct{}),
	}

	builder := component.NewComponentManagerBuilder()

	builder.AddWorker(p.publishWorker)

	p.cm = builder.Build()
	p.Component = p.cm

	return p
}

func (p *Publisher[T]) Publish(data T) {
	select {
	case <-p.publisherExited:
		return
	default:
		p.publishChan <- data
	}
}

func (p *Publisher[T]) Subscribe(s Subscriber[T]) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.subscribers[s] = struct{}{}
}

func (p *Publisher[T]) Unsubscribe(s Subscriber[T]) {
	p.mux.Lock()
	defer p.mux.Unlock()

	delete(p.subscribers, s)
}

func (p *Publisher[T]) publishWorker(ctx irrecoverable.SignalerContext, ready component.ReadyFunc) {
	defer func() {
		close(p.publisherExited)
		close(p.publishChan)
	}()
	ready()

	for {
		select {
		case <-ctx.Done():
			return
		case data := <-p.publishChan:
			stop := func() bool {
				p.mux.RLock()
				defer p.mux.RUnlock()

				for s := range p.subscribers {
					err := s.Notify(data)
					if err != nil {
						p.log.Error().Err(err).Msg("failed to notify subscriber")
						ctx.Throw(err)
						return true
					}
				}
				return false
			}()
			if stop {
				return
			}
		}
	}
}

type Subscriber[T any] interface {
	Notify(data T) error
}

type Subscription[T any] struct {
	callback func(data T) error
}

func NewSubscription[T any](callback func(T) error) *Subscription[T] {
	return &Subscription[T]{
		callback: callback,
	}
}

func (b *Subscription[T]) Notify(data T) error {
	return b.callback(data)
}
