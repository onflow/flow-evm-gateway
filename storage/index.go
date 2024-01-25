package storage

type BlockIndex interface {
	Store(block any, height uint64) error
	Get(height uint64) (any, error)
	LatestHeight() (uint64, error)
	FirstHeight() (uint64, error)
}

type LogsIndex interface {
	Store(any) error
	Get(topic string) (any, error)
}
