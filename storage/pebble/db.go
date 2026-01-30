package pebble

import (
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	"github.com/rs/zerolog/log"
)

// OpenDB opens a new pebble database at the provided directory.
func OpenDB(dir string) (*pebble.DB, error) {
	cache := pebble.NewCache(1 << 20)
	defer cache.Unref()

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
	return db, nil
}

func WithBatch(store *Storage, f func(batch *pebble.Batch) error) error {
	batch := store.NewBatch()
	defer func(batch *pebble.Batch) {
		err := batch.Close()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to close batch")
		}
	}(batch)

	err := f(batch)
	if err != nil {
		return err
	}

	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}
