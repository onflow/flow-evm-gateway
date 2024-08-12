package pebble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

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

	if batch != nil {
		// set the value on batch and return
		return batch.Set(prefixedKey, value, nil)
	}

	return s.db.Set(prefixedKey, value, nil)
}

func (s *Storage) get(keyCode byte, key ...[]byte) ([]byte, error) {
	prefixedKey := makePrefix(keyCode, key...)

	data, closer, err := s.db.Get(prefixedKey)
	// temp workaround to a weird bug where the data changes after returned
	cp := make([]byte, len(data))
	copy(cp, data)

	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, errs.ErrNotFound
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

// batchGet loads the value from an indexed batch if data is found, else it loads the value from the storage.
func (s *Storage) batchGet(batch *pebble.Batch, keyCode byte, key ...[]byte) ([]byte, error) {
	if batch == nil || !batch.Indexed() {
		return nil, fmt.Errorf("batch must not be nil and it must be indexed")
	}

	data, closer, err := batch.Get(makePrefix(keyCode, key...))
	if err == nil {
		_ = closer.Close()
		return data, nil
	}

	return s.get(keyCode, key...)
}

func (s *Storage) NewBatch() *pebble.Batch {
	return s.db.NewIndexedBatch()
}

// Delete all DB entries after the given height, setting this as the latest
// indexed height.
func (s *Storage) ResetAtHeight(cadenceHeight uint64) error {
	// convert the given cadenceHeight to its EVM height
	val, err := s.get(cadenceHeightToEVMHeightKey, uint64Bytes(cadenceHeight))
	if err != nil {
		return err
	}
	evmHeight := binary.BigEndian.Uint64(val)

	blocks := NewBlocks(s)
	receipts := NewReceipts(s)

	currentCadenceHeight := cadenceHeight
	currentEVMHeight := evmHeight

	latestEVMHeight, err := blocks.LatestEVMHeight()
	if err != nil {
		return err
	}

	// fetch blocks and delete their transactions/receipts and other keys
	for currentEVMHeight <= latestEVMHeight {
		block, err := blocks.GetByHeight(currentEVMHeight)
		if err != nil {
			return err
		}

		// delete all transactions of the block
		txHashes := block.TransactionHashes
		for _, txHash := range txHashes {
			// t.store.set(txIDKey, txHash.Bytes(), val, batch)
			txKey := makePrefix(txIDKey, txHash.Bytes())
			err := s.db.Delete(txKey, nil)
			if err != nil {
				return err
			}
			fmt.Println("Deleted TX: ", txHash)

			// delete all traces of the block
			// t.store.set(traceTxIDKey, ID.Bytes(), trace, batch)
			traceKey := makePrefix(traceTxIDKey, txHash.Bytes())
			err = s.db.Delete(traceKey, nil)
			if err != nil {
				return err
			}
			fmt.Println("Deleted Trace: ", txHash)

			// we cannot delete the nonce for each account, because of
			// a.store.set(accountNonceKey, from.Bytes(), data, batch)
			// we do not know the `from`, in order to construct the
			// prefixed key.

			// r.store.set(receiptTxIDToHeightKey, receipt.TxHash.Bytes(), height, batch)
			receipt, err := receipts.GetByTransactionID(txHash)
			if err != nil {
				return err
			}
			fmt.Println("Deleted Receipt: ", txHash)

			receiptKey := makePrefix(receiptTxIDToHeightKey, receipt.TxHash.Bytes())
			err = s.db.Delete(receiptKey, nil)
			if err != nil {
				return err
			}
			fmt.Println("Deleted receiptKey: ", receiptKey)

			// r.store.set(receiptHeightKey, height, val, batch)
			height := receipt.BlockNumber.Bytes()
			err = s.db.Delete(makePrefix(receiptHeightKey, height), nil)
			if err != nil {
				return err
			}
			fmt.Println("Deleted receipt height: ", receipt.BlockNumber.Uint64())

			// r.store.set(bloomHeightKey, height, bloomVal, batch)
			err = s.db.Delete(makePrefix(bloomHeightKey, height), nil)
			if err != nil {
				return err
			}
			fmt.Println("Deleted receipt blooms: ", receipt.BlockNumber.Uint64())
		}

		cadenceHeightBytes := uint64Bytes(currentCadenceHeight)
		evmHeightBytes := uint64Bytes(block.Height)

		// b.store.set(blockHeightKey, evmHeightBytes, val, batch)
		err = s.db.Delete(makePrefix(blockHeightKey, evmHeightBytes), nil)
		if err != nil {
			return err
		}

		id, err := block.Hash()
		if err != nil {
			return err
		}

		// b.store.set(blockIDToHeightKey, id.Bytes(), evmHeightBytes, batch)
		err = s.db.Delete(makePrefix(blockIDToHeightKey, id.Bytes()), nil)
		if err != nil {
			return err
		}

		// b.store.set(evmHeightToCadenceHeightKey, evmHeightBytes, cadenceHeightBytes, batch)
		err = s.db.Delete(makePrefix(evmHeightToCadenceHeightKey, evmHeightBytes), nil)
		if err != nil {
			return err
		}

		// b.store.set(cadenceHeightToEVMHeightKey, cadenceHeightBytes, evmHeightBytes, batch)
		err = s.db.Delete(makePrefix(cadenceHeightToEVMHeightKey, cadenceHeightBytes), nil)
		if err != nil {
			return err
		}

		// b.store.set(evmHeightToCadenceIDKey, evmHeightBytes, cadenceID.Bytes(), batch)
		err = s.db.Delete(makePrefix(evmHeightToCadenceIDKey, evmHeightBytes), nil)
		if err != nil {
			return err
		}

		fmt.Println("Deleted EVM Height: ", currentEVMHeight)
		fmt.Println("Deleted Cadence Height: ", currentCadenceHeight)
		currentEVMHeight += 1
		currentCadenceHeight += 1
	}

	err = s.set(latestCadenceHeightKey, nil, uint64Bytes(cadenceHeight), nil)
	if err != nil {
		return err
	}

	err = s.set(latestEVMHeightKey, nil, uint64Bytes(evmHeight), nil)
	if err != nil {
		return err
	}

	fmt.Println("Set Latest EVM Height: ", evmHeight)
	fmt.Println("Set Latest Cadence Height: ", cadenceHeight)

	return nil
}
