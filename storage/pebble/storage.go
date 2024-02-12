package pebble

import (
	"errors"
	"fmt"
	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/rlp"
	errs "github.com/onflow/flow-evm-gateway/storage/errors"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/rs/zerolog"
	"io"
)

type Storage struct {
	db  *pebble.DB
	log zerolog.Logger
}

// New creates a new storage instance using the provided dir location as the storage directory.
func New(dir string, log zerolog.Logger) (*Storage, error) {
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

	db, err := pebble.Open(dir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	return &Storage{db: db, log: log}, nil
}

func (s *Storage) set(keyCode byte, key any, value []byte) error {
	// by default, we disable sync since write operations are idempotent and since crash is not expected to be common
	// we can rely on idempotency to resolve such a crash and gain performance benefits of having sync off
	writeOpts := &pebble.WriteOptions{Sync: false}

	prefixedKey := makePrefix(keyCode, key)
	return s.db.Set(prefixedKey, value, writeOpts)
}

func (s *Storage) get(keyCode byte, key any) ([]byte, error) {
	prefixedKey := makePrefix(keyCode, key)

	data, closer, err := s.db.Get(prefixedKey)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, errs.NotFound
		}
		return nil, err
	}

	defer func(closer io.Closer) {
		err = closer.Close()
		if err != nil {
			s.log.Error().Err(err)
		}
	}(closer)

	return data, nil
}

func (s *Storage) storeBlock(block *types.Block) error {
	val, err := block.ToBytes()
	if err != nil {
		return err
	}

	return s.set(blockHeightKey, block.Height, val)
}

func (s *Storage) getBlock(height uint64) (*types.Block, error) {
	data, err := s.get(blockHeightKey, height)
	if err != nil {
		return nil, err
	}

	var block types.Block
	err = rlp.DecodeBytes(data, &block)
	if err != nil {
		return nil, err
	}

	return &block, nil
}
