package pebble

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/cockroachdb/pebble"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/rs/zerolog"

	errs "github.com/onflow/flow-evm-gateway/storage/errors"
)

type Storage struct {
	db    *pebble.DB
	log   zerolog.Logger
	cache *lru.TwoQueueCache[string, []byte]
}

// New creates a new storage instance using the provided dir location as the storage directory.
func New(dir string, log zerolog.Logger) (*Storage, error) {
	cache := pebble.NewCache(1 << 20)
	defer cache.Unref()

	log = log.With().Str("component", "storage").Logger()

	// currently pebble is only used for registers
	opts := &pebble.Options{
		Cache:                 cache,
		FormatMajorVersion:    pebble.FormatNewest,
		L0CompactionThreshold: 2,
		L0StopWritesThreshold: 1000,
		// When the maximum number of bytes for a level is exceeded, compaction is requested.
		LBaseMaxBytes: 64 << 20, // 64 MB
		Levels:        make([]pebble.LevelOptions, 7),
		MaxOpenFiles:  16384,
		// Writes are stopped when the sum of the queued memtable sizes exceeds MemTableStopWritesThreshold*MemTableSize.
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
		// The default is 1.
		MaxConcurrentCompactions: func() int { return 4 },
	}

	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		// The default is 4KiB (uncompressed), which is too small
		// for good performance (esp. on stripped storage).
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		if i > 0 {
			// L0 starts at 2MiB, each level is 2x the previous.
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
		}
		l.EnsureDefaults()
	}

	// Splitting sstables during flush allows increased compaction flexibility and concurrency when those
	// tables are compacted to lower levels.
	opts.FlushSplitBytes = opts.Levels[0].TargetFileSize
	opts.EnsureDefaults()

	db, err := pebble.Open(dir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	valueCache, err := lru.New2Q[string, []byte](128)
	if err != nil {
		return nil, err
	}

	return &Storage{
		db:    db,
		log:   log,
		cache: valueCache,
	}, nil
}

// set key-value pair identified by key code (which act as an entity identifier).
//
// Optional batch argument makes the operation atomic, but it's up to the caller to
// commit the batch or revert it.
func (s *Storage) set(keyCode byte, key []byte, value []byte, batch *pebble.Batch) error {
	prefixedKey := makePrefix(keyCode, key)

	if batch != nil {
		// set the value on batch and return, skip caching as the commit did not happen yet
		return batch.Set(prefixedKey, value, nil)
	}

	if err := s.db.Set(prefixedKey, value, nil); err != nil {
		return err
	}

	s.cache.Add(hex.EncodeToString(prefixedKey), value)
	return nil
}

func (s *Storage) get(keyCode byte, key ...[]byte) ([]byte, error) {
	prefixedKey := makePrefix(keyCode, key...)

	// check if we have the value cached and return it
	if val, ok := s.cache.Get(hex.EncodeToString(prefixedKey)); ok {
		return val, nil
	}

	// if cache was a miss, then load the value from db
	data, closer, err := s.db.Get(prefixedKey)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}

	// add the value to the cache
	s.cache.Add(hex.EncodeToString(prefixedKey), data)

	defer func(closer io.Closer) {
		err = closer.Close()
		if err != nil {
			s.log.Error().Err(err).Msg("failed to close storage")
		}
	}(closer)

	return data, nil
}

func (s *Storage) NewBatch() *pebble.Batch {
	return s.db.NewBatch()
}
