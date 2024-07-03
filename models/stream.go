package models

import (
	"sync"

	"github.com/google/uuid"
)

/*
publisher 	-> publish(block1)
			-> publish(block2)

publisher	-> subscribe(subscriber1)

subscriber1	-> notify()
*/

type Publisher struct {
	mux         sync.Mutex
	subscribers map[uuid.UUID]Subscriber
}

func (p *Publisher) Publish(data any) {
	for _, s := range p.subscribers {
		s.Notify(data)
	}
}

func (p *Publisher) Subscribe(s Subscriber) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.subscribers[s.ID()] = s
}

func (p *Publisher) Unsubscribe(s Subscriber) {
	p.mux.Lock()
	defer p.mux.Unlock()

	delete(p.subscribers, s.ID())
}

type Subscriber interface {
	ID() uuid.UUID
	Notify(data any)
	Error() <-chan error
}

type Subscription struct {
	err      chan error
	callback func(data any) error
	uuid     uuid.UUID
}

func NewSubscription(callback func(any) error) *Subscription {
	return &Subscription{
		callback: callback,
		uuid:     uuid.New(),
	}
}

func (b *Subscription) Notify(data any) {
	err := b.callback(data)
	if err != nil {
		b.err <- err
	}
}

func (b *Subscription) ID() uuid.UUID {
	return b.uuid
}

func (b *Subscription) Error() <-chan error {
	return b.err
}
