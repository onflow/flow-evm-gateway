package pebble

import (
	"errors"
	"fmt"
	"io"

	"github.com/cockroachdb/pebble/bloom"

	"github.com/cockroachdb/pebble"
	"github.com/rs/zerolog"

	errs "github.com/onflow/flow-evm-gateway/models/errors"
)

type Storage struct {
	db  *pebble.DB
	log zerolog.Logger
}

// New creates a new storage instance using the provided dir location as the storage directory.
func New(dir string, log zerolog.Logger) (*Storage, error) {
	cache := pebble.NewCache(1 << 20)
	defer cache.Unref()

	log = log.With().Str("component", "storage").Logger()

	// currently pebble is only used for registers
	opts := &pebble.Options{
		Cache:                 cache,
		Comparer:              NewMVCCComparer(),
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

		// The bloom filter speedsup our SeekPrefixGE by skipping
		// sstables that do not contain the prefix
		l.FilterPolicy = bloom.FilterPolicy(MinLookupKeyLen)
		l.FilterType = pebble.TableFilter

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
		return nil, fmt.Errorf("failed to open db for dir: %s, with: %w", dir, err)
	}

	return &Storage{
		db:  db,
		log: log,
	}, nil
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

func (s *Storage) NewBatch() *pebble.Batch {
	return s.db.NewBatch()
}

func (s *Storage) Close() error {
	return s.db.Close()
}
