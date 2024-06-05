package traces

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

func TestTraceIngestion(t *testing.T) {
	blockBroadcaster := broadcast.NewBroadcaster()
	blocks := &storageMock.BlockIndexer{}
	trace := &storageMock.TraceIndexer{}
	downloader := &mocks.Downloader{}

	txTrace := json.RawMessage(`{
	   "from":"0x42fdd562221741a1db62a0f69a5a680367f07e33",
	   "gas":"0x15f900",
	   "gasUsed":"0x387dc",
	   "to":"0xca11bde05977b3631167028862be2a173976ca11" 
	}`)

	latestHeight := uint64(0)
	blockID := flow.Identifier{0x01, 0x02}
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

	index := 0
	downloader.
		On("Download", mock.Anything, mock.Anything).
		Return(func(txID gethCommon.Hash, blkID flow.Identifier) (json.RawMessage, error) {
			require.Equal(t, hashes[index].String(), txID.String())
			require.Equal(t, blockID, blkID)

			index++
			return txTrace, nil
		})

	trace.
		On("StoreTransaction", mock.Anything, mock.Anything).
		Return(func(ID common.Hash, trace json.RawMessage) error {
			require.Equal(t, blockID.String(), ID.String())
			require.Equal(t, txTrace, trace)
			return nil
		})

	engine := NewTracesIngestionEngine(latestHeight, blockBroadcaster, blocks, trace, downloader, zerolog.Nop())

	err := engine.Run(context.Background())
	require.NoError(t, err)

	blockBroadcaster.Publish()
}
