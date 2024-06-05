package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/onflow/flow-go-sdk"
	broadcast "github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/onflow/flow-evm-gateway/services/traces/mocks"
	storageMock "github.com/onflow/flow-evm-gateway/storage/mocks"
)

// this test makes sure once a notification for a new block is triggered
// the block transaction hashes are iterated, and for each a trace is
// downloaded and stored.
func TestTraceIngestion(t *testing.T) {
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
}
