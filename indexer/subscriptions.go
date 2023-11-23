package indexer

type Subscription[T any] struct {
	ch  chan T
	err error
}

func NewSubscription[T any]() *Subscription[T] {
	return &Subscription[T]{
		ch: make(chan T),
	}
}

func (s *Subscription[T]) Channel() <-chan T {
	return s.ch
}

func (s *Subscription[T]) Err() error {
	return s.err
}
