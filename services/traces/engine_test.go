package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/onflow/flow-go-sdk"
	broadcast "github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/services/traces/mocks"
	storageMock "github.com/onflow/flow-evm-gateway/storage/mocks"
)

// this test makes sure once a notification for a new block is triggered
// the block transaction hashes are iterated, and for each a trace is
// downloaded and stored.
func TestTraceIngestion(t *testing.T) {
	t.Run("successful single block ingestion", func(t *testing.T) {
		blockBroadcaster := broadcast.NewBroadcaster()
		blocks := &storageMock.BlockIndexer{}
		trace := &storageMock.TraceIndexer{}
		downloader := &mocks.Downloader{}

		txTrace := func(id gethCommon.Hash) json.RawMessage {
			return json.RawMessage(fmt.Sprintf(`{
				"id": "%s",
			   "from":"0x42fdd562221741a1db62a0f69a5a680367f07e33",
			   "gas":"0x15f900",
			   "gasUsed":"0x387dc",
			   "to":"0xca11bde05977b3631167028862be2a173976ca11" 
			}`, id.String()))
		}

		latestHeight := uint64(0)
		blockID := flow.Identifier{0x09}
		hashes := []gethCommon.Hash{{0x1}, {0x2}, {0x3}}

		blocks.
			On("GetByHeight", mock.Anything).
			Return(func(height uint64) (*types.Block, error) {
				require.Equal(t, latestHeight+1, height) // make sure it gets next block
				block := storageMock.NewBlock(height)
				block.TransactionHashes = hashes
				return block, nil
			})

		blocks.
			On("GetCadenceID", mock.Anything).
			Return(func(height uint64) (flow.Identifier, error) {
				require.Equal(t, latestHeight+1, height)
				return blockID, nil
			})

		downloadedHashes := make(map[gethCommon.Hash]struct{})
		downloader.
			On("Download", mock.Anything, mock.Anything).
			Return(func(txID gethCommon.Hash, blkID flow.Identifier) (json.RawMessage, error) {
				require.Equal(t, blockID, blkID)
				downloadedHashes[txID] = struct{}{}
				time.Sleep(time.Millisecond * 200) // simulate download delay
				return txTrace(txID), nil
			})

		stored := make(chan gethCommon.Hash, len(hashes))
		trace.
			On("StoreTransaction", mock.Anything, mock.Anything).
			Return(func(ID gethCommon.Hash, trace json.RawMessage) error {
				require.Equal(t, txTrace(ID), trace)
				stored <- ID
				return nil
			})

		engine := NewTracesIngestionEngine(latestHeight, blockBroadcaster, blocks, trace, downloader, zerolog.Nop())

		err := engine.Run(context.Background())
		require.NoError(t, err)

		blockBroadcaster.Publish()

		// make sure stored was called as many times as block contained hashes
		require.Eventuallyf(t, func() bool {
			return len(stored) == len(hashes)
		}, time.Second, time.Millisecond*50, "index not run")

		close(stored)
		storedHashes := make([]string, 0)
		for h := range stored {
			storedHashes = append(storedHashes, h.String())
		}

		// make sure we stored all the hashes in the block
		for _, h := range hashes {
			require.True(t, slices.Contains(storedHashes, h.String()))
		}
	})

	t.Run("successful multiple blocks ingestion", func(t *testing.T) {
		blockBroadcaster := broadcast.NewBroadcaster()
		blocks := &storageMock.BlockIndexer{}
		trace := &storageMock.TraceIndexer{}
		downloader := &mocks.Downloader{}

		txTrace := func(id gethCommon.Hash) json.RawMessage {
			return json.RawMessage(fmt.Sprintf(`{
				"id": "%s",
			   "from":"0x42fdd562221741a1db62a0f69a5a680367f07e33",
			   "gas":"0x15f900",
			   "gasUsed":"0x387dc",
			   "to":"0xca11bde05977b3631167028862be2a173976ca11" 
			}`, id.String()))
		}

		latestHeight := uint64(0)

		const blockCount = 10
		const txCount = 50

		// generate mock blocks, each with mock transactions
		mockBlocks := make([]*types.Block, blockCount+1)
		mockCadenceIDs := make([]flow.Identifier, blockCount+1)

		for i := range mockBlocks {
			b := storageMock.NewBlock(uint64(i))
			cid := flow.Identifier{byte(i + 10)}

			h := make([]gethCommon.Hash, txCount)
			for j := range h {
				h[j] = gethCommon.Hash{byte(j), byte(i)}
			}

			b.TransactionHashes = h
			mockBlocks[i] = b
			mockCadenceIDs[i] = cid
		}

		blocks.
			On("GetByHeight", mock.Anything).
			Return(func(height uint64) (*types.Block, error) {
				latestHeight++
				require.Equal(t, latestHeight, height) // make sure it gets next block
				require.Less(t, int(height), len(mockBlocks))
				return mockBlocks[height], nil
			})

		blocks.
			On("GetCadenceID", mock.Anything).
			Return(func(height uint64) (flow.Identifier, error) {
				require.Equal(t, latestHeight, height)
				require.Less(t, int(height), len(mockCadenceIDs))
				return mockCadenceIDs[height], nil
			})

		downloadedIDs := make(chan string, blockCount*txCount)
		downloader.
			On("Download", mock.Anything, mock.Anything).
			Return(func(txID gethCommon.Hash, blkID flow.Identifier) (json.RawMessage, error) {
				id := fmt.Sprintf("%s-%s", blkID.String(), txID.String())
				downloadedIDs <- id
				time.Sleep(time.Millisecond * 200) // simulate download delay
				return txTrace(txID), nil
			})

		stored := make(chan gethCommon.Hash, blockCount*txCount)
		trace.
			On("StoreTransaction", mock.Anything, mock.Anything).
			Return(func(ID gethCommon.Hash, trace json.RawMessage) error {
				require.Equal(t, txTrace(ID), trace)
				stored <- ID
				return nil
			})

		engine := NewTracesIngestionEngine(latestHeight, blockBroadcaster, blocks, trace, downloader, zerolog.Nop())

		err := engine.Run(context.Background())
		require.NoError(t, err)

		for i := 0; i < blockCount; i++ {
			blockBroadcaster.Publish()
			time.Sleep(time.Millisecond * 100) // simulate block delay
		}

		// make sure download was called as many times as all blocks times the hashes it contained
		require.Eventuallyf(t, func() bool {
			return len(downloadedIDs) == blockCount*txCount
		}, time.Second*10, time.Millisecond*100, "traces not downloaded")

		close(downloadedIDs)

		// make sure stored was called as many times as all blocks times the hashes it contained
		require.Eventuallyf(t, func() bool {
			return len(stored) == blockCount*txCount
		}, time.Second*10, time.Millisecond*100, "traces not indexed")

		close(stored)

		// make sure we downloaded and indexed all the hashes in the block
		for id := range downloadedIDs {
			found := false
			for _, b := range mockBlocks {
				for _, h := range b.TransactionHashes {
					txID := strings.Split(id, "-")[1]
					if txID == h.String() {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			require.True(t, found, fmt.Sprintf("id %s not found", id))
		}
	})

	t.Run("failed download retries", func(t *testing.T) {
		blockBroadcaster := broadcast.NewBroadcaster()
		blocks := &storageMock.BlockIndexer{}
		downloader := &mocks.Downloader{}
		trace := &storageMock.TraceIndexer{}
		logger := zerolog.New(zerolog.NewTestWriter(t))

		latestHeight := uint64(0)
		blockID := flow.Identifier{0x09}
		hashes := []gethCommon.Hash{{0x1}}

		blocks.
			On("GetByHeight", mock.Anything).
			Return(func(height uint64) (*types.Block, error) {
				require.Equal(t, latestHeight+1, height) // make sure it gets next block
				block := storageMock.NewBlock(height)
				block.TransactionHashes = hashes
				return block, nil
			})

		blocks.
			On("GetCadenceID", mock.Anything).
			Return(func(height uint64) (flow.Identifier, error) {
				require.Equal(t, latestHeight+1, height)
				return blockID, nil
			})

		const retriesNum = 3
		downloads := make(chan struct{}, retriesNum)
		downloader.
			On("Download", mock.Anything, mock.Anything).
			Return(func(txID gethCommon.Hash, blkID flow.Identifier) (json.RawMessage, error) {
				downloads <- struct{}{}
				return nil, fmt.Errorf("failed download")
			})

		engine := NewTracesIngestionEngine(latestHeight, blockBroadcaster, blocks, trace, downloader, logger)

		err := engine.Run(context.Background())
		require.NoError(t, err)

		blockBroadcaster.Publish()

		// make sure stored was called as many times as block contained hashes
		require.Eventuallyf(t, func() bool {
			return len(downloads) == retriesNum
		}, time.Second*10, time.Millisecond*200, "download not retried")

		close(downloads)
	})
}
