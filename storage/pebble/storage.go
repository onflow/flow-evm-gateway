package pebble

import (
	"errors"
	"io"

	"github.com/cockroachdb/pebble"
	"github.com/rs/zerolog"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

type Storage struct {
	db  *pebble.DB
	log zerolog.Logger
}

// New creates a new storage instance using the provided db.
func New(db *pebble.DB, log zerolog.Logger) *Storage {
	return &Storage{
		db:  db,
		log: log,
	}
}

// set key-value pair identified by key code (which act as an entity identifier).
//
// Optional batch argument makes the operation atomic, but it's up to the caller to
// commit the batch or revert it.
func (s *Storage) set(keyCode byte, key []byte, value []byte, batch *pebble.Batch) error {
	prefixedKey := makePrefix(keyCode, key)

	return batch.Set(prefixedKey, value, nil)
}

func (s *Storage) get(keyCode byte, key ...[]byte) ([]byte, error) {
	prefixedKey := makePrefix(keyCode, key...)

	data, closer, err := s.db.Get(prefixedKey)
	// temp workaround to a weird bug where the data changes after returned
	cp := make([]byte, len(data))
	copy(cp, data)

	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, errs.ErrEntityNotFound
		}
		return nil, err
	}

	defer func(closer io.Closer) {
		err = closer.Close()
		if err != nil {
			s.log.Error().Err(err).Msg("failed to close storage")
		}
	}(closer)

	return cp, nil
}

func (s *Storage) delete(keyCode byte, key []byte, batch *pebble.Batch) error {
	prefixedKey := makePrefix(keyCode, key)

	return batch.Delete(prefixedKey, nil)
}

func (s *Storage) NewBatch() *pebble.Batch {
	return s.db.NewBatch()
}
