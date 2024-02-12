package pebble

import (
	"fmt"
	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/onflow/flow-go/fvm/evm/types"
	"io"
)

type Storage struct {
	db *pebble.DB
}

func New() (*Storage, error) {
	cache := pebble.NewCache(1 << 20)
	defer cache.Unref()

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

	dbDir := "./db"
	db, err := pebble.Open(dbDir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	return &Storage{db: db}, nil
}

func (s *Storage) storeBlock(block *types.Block) error {
	key := []byte(fmt.Sprintf("blocks/%d", block.Height))
	val, err := block.ToBytes()
	if err != nil {
		return err
	}

	// we make the sync false since we can always repeat the indexing in case of crash which should be rare so the performance optimization is worth it
	return s.db.Set(key, val, &pebble.WriteOptions{Sync: false})
}

func (s *Storage) getBlock(height uint64) (*types.Block, error) {
	key := []byte(fmt.Sprintf("blocks/%d", height))
	data, closer, err := s.db.Get(key)
	if err != nil {
		return nil, err
	}
	defer func(closer io.Closer) {
		err = closer.Close()
		if err != nil {

		}
	}(closer)

	var block types.Block
	err = rlp.DecodeBytes(data, &block)
	if err != nil {
		return nil, err
	}

	return &block, nil
}
