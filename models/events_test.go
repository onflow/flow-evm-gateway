package models

import (
	"math/big"
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCadenceEvents_Block(t *testing.T) {
	invalid := cadence.String("invalid")

	b0, e0, err := newBlock(0, nil)
	require.NoError(t, err)

	tests := []struct {
		name   string
		events flow.BlockEvents
		block  *Block
		err    error
	}{
		{
			name:   "BlockExecutedEventExists",
			events: flow.BlockEvents{Events: []flow.Event{e0}},
			block:  b0,
		}, {
			name:   "BlockExecutedEventEmpty",
			events: flow.BlockEvents{Events: []flow.Event{}},
			block:  nil,
		}, {
			name: "BlockExecutedNotFound",
			events: flow.BlockEvents{Events: []flow.Event{{
				Type:  e0.Type,
				Value: cadence.NewEvent([]cadence.Value{invalid}),
			}}},
			block: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := NewCadenceEvents(tt.events)
			require.NoError(t, err)

			if tt.block != nil {
				ttHash, err := tt.block.Hash()
				require.NoError(t, err)
				hash, err := e.Block().Hash()
				require.NoError(t, err)
				assert.Equal(t, ttHash, hash)
			} else {
				assert.Nil(t, e.Block())
			}
		})
	}

	cadenceHeight := uint64(1)
	txCount := 10
	hashes := make([]gethCommon.Hash, txCount)
	events := make([]flow.Event, 0)

	// generate txs
	for i := 0; i < txCount; i++ {
		tx, _, txEvent, err := newTransaction(uint64(i), uint16(i))
		require.NoError(t, err)
		hashes[i] = tx.Hash()
		events = append(events, txEvent)
	}

	t.Run("block with less transaction hashes", func(t *testing.T) {
		// generate single block
		_, blockEvent, err := newBlock(cadenceHeight, hashes[:txCount-2])
		require.NoError(t, err)

		blockEvents := flow.BlockEvents{
			BlockID: flow.Identifier{0x1},
			Height:  cadenceHeight,
			Events:  events,
		}

		blockEvents.Events = append(blockEvents.Events, blockEvent)

		_, err = NewCadenceEvents(blockEvents)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"block 1 references missing transaction/s",
		)
	})

	t.Run("block with equal transaction hashes", func(t *testing.T) {
		// generate single block
		_, blockEvent, err := newBlock(cadenceHeight, hashes)
		require.NoError(t, err)

		blockEvents := flow.BlockEvents{
			BlockID: flow.Identifier{0x1},
			Height:  cadenceHeight,
			Events:  events,
		}

		blockEvents.Events = append(blockEvents.Events, blockEvent)

		_, err = NewCadenceEvents(blockEvents)
		require.NoError(t, err)
	})

	t.Run("block with empty transaction hashes", func(t *testing.T) {
		// generate single block
		_, blockEvent, err := newBlock(cadenceHeight, []gethCommon.Hash{})
		require.NoError(t, err)

		blockEvents := flow.BlockEvents{
			BlockID: flow.Identifier{0x1},
			Height:  cadenceHeight,
		}

		blockEvents.Events = append(blockEvents.Events, blockEvent)

		_, err = NewCadenceEvents(blockEvents)
		require.NoError(t, err)
	})

	t.Run("block with more transaction hashes", func(t *testing.T) {
		tx, _, _, err := newTransaction(1, 0)
		require.NoError(t, err)

		// generate single block
		_, blockEvent, err := newBlock(cadenceHeight, []gethCommon.Hash{tx.Hash()})
		require.NoError(t, err)

		blockEvents := flow.BlockEvents{
			BlockID: flow.Identifier{0x1},
			Height:  cadenceHeight,
		}

		blockEvents.Events = append(blockEvents.Events, blockEvent)

		_, err = NewCadenceEvents(blockEvents)
		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"block 1 references missing transaction/s",
		)
	})

	t.Run("EVM events are ordered by Flow TransactionIndex & EventIndex", func(t *testing.T) {
		t.Skip()
		txCount := 3
		blockEvents := flow.BlockEvents{
			BlockID: flow.Identifier{0x1},
			Height:  1,
		}

		// tx1 and tx2 are EVM transactions executed on a single Flow transaction.
		tx1, _, txEvent1, err := newTransaction(0, 0)
		require.NoError(t, err)
		txEvent1.TransactionIndex = 0
		txEvent1.EventIndex = 2

		tx2, _, txEvent2, err := newTransaction(1, 1)
		require.NoError(t, err)
		txEvent2.TransactionIndex = 0
		txEvent2.EventIndex = 5

		// tx3 is a Flow transaction with a single EVM transaction on EventIndex=1
		tx3, _, txEvent3, err := newTransaction(2, 0)
		require.NoError(t, err)
		txEvent3.TransactionIndex = 2
		txEvent3.EventIndex = 1

		// needed for computing the `TransactionHashRoot` field on
		// EVM.BlockExecuted event payload. the order is sensitive.
		hashes = []gethCommon.Hash{
			tx1.Hash(),
			tx2.Hash(),
			tx3.Hash(),
		}

		// add the tx events in a shuffled order
		blockEvents.Events = []flow.Event{
			txEvent3,
			txEvent1,
			txEvent2,
		}

		// generate single block
		block, blockEvent, err := newBlock(1, hashes)
		require.NoError(t, err)
		blockEvent.TransactionIndex = 4
		blockEvent.EventIndex = 0
		blockEvents.Events = append(blockEvents.Events, blockEvent)

		// parse the EventStreaming API response
		cdcEvents, err := NewCadenceEvents(blockEvents)
		require.NoError(t, err)

		// assert that Flow events are sorted by their TransactionIndex and EventIndex fields
		assert.Equal(
			t,
			[]flow.Event{
				txEvent1,
				txEvent2,
				txEvent3,
				blockEvent,
			},
			cdcEvents.events.Events,
		)

		// assert we have collected the EVM.BlockExecuted event payload
		blockEventPayload := cdcEvents.BlockEventPayload()
		blockHash, err := block.Hash()
		require.NoError(t, err)
		assert.Equal(t, blockHash, blockEventPayload.Hash)

		// assert that EVM transactions & receipts are sorted by their
		// TransactionIndex field
		for i := 0; i < txCount; i++ {
			tx := cdcEvents.transactions[i]
			receipt := cdcEvents.receipts[i]
			assert.Equal(t, tx.Hash(), receipt.TxHash)
			assert.Equal(t, uint(i), receipt.TransactionIndex)

			// assert we have collected the EVM.TransactionExecuted event payloads
			// in their correct order.
			txEventPayload := cdcEvents.TxEventPayloads()[i]
			assert.Equal(t, tx.Hash(), txEventPayload.Hash)
			assert.Equal(t, blockEventPayload.Height, txEventPayload.BlockHeight)
		}
	})
}

func Test_EventDecoding(t *testing.T) {
	cadenceHeight := uint64(1)
	txCount := 10
	txEvents := make([]flow.Event, txCount)
	txs := make([]Transaction, txCount)
	hashes := make([]gethCommon.Hash, txCount)
	results := make([]*types.Result, txCount)

	blockEvents := flow.BlockEvents{
		BlockID: flow.Identifier{0x1},
		Height:  cadenceHeight,
	}

	// generate txs
	for i := 0; i < txCount; i++ {
		var err error
		txs[i], results[i], txEvents[i], err = newTransaction(uint64(i), uint16(i))
		require.NoError(t, err)
		hashes[i] = txs[i].Hash()
		blockEvents.Events = append(blockEvents.Events, txEvents[i])
	}

	// generate single block
	block, blockEvent, err := newBlock(1, hashes)
	require.NoError(t, err)
	blockEvents.Events = append(blockEvents.Events, blockEvent)

	cadenceEvents, err := NewCadenceEvents(blockEvents)
	require.NoError(t, err)

	assert.Equal(t, block, cadenceEvents.Block())
	assert.False(t, cadenceEvents.Empty())
	assert.Equal(t, cadenceHeight, cadenceEvents.CadenceHeight())
	assert.Equal(t, cadenceEvents.Block().TransactionHashes, hashes)

	require.Equal(t, txCount+1, cadenceEvents.Length()) // +1 is for block event
	require.Equal(t, txCount, len(cadenceEvents.Receipts()))
	require.Equal(t, txCount, len(cadenceEvents.Transactions()))

	cumulative := uint64(1)
	logIndex := uint(0)
	for i := 0; i < txCount; i++ {
		tx := cadenceEvents.Transactions()[i]
		rcp := cadenceEvents.Receipts()[i]
		blockHash, err := block.Hash()
		require.NoError(t, err)
		resRcp := results[i].Receipt()

		assert.Equal(t, txs[i].Hash(), tx.Hash())
		assert.Equal(t, txs[i].To(), tx.To())
		assert.Equal(t, blockHash, rcp.BlockHash)
		assert.Equal(t, block.Height, rcp.BlockNumber.Uint64())
		assert.Equal(t, resRcp.Status, rcp.Status)
		assert.Equal(t, cumulative, rcp.CumulativeGasUsed)
		assert.Equal(t, tx.Hash(), rcp.TxHash)
		assert.Equal(t, uint(i), rcp.TransactionIndex)

		for _, l := range rcp.Logs {
			assert.Equal(t, tx.Hash(), l.TxHash)
			assert.Equal(t, block.Height, l.BlockNumber)
			assert.Equal(t, rcp.TransactionIndex, l.TxIndex)
			assert.Equal(t, logIndex, l.Index)
			logIndex++
		}

		cumulative += uint64(1) // we make each tx use 1 gas, so cumulative just adds 1
	}
}

func newTransaction(nonce uint64, txIndex uint16) (Transaction, *types.Result, flow.Event, error) {
	tx := gethTypes.NewTransaction(
		nonce,
		gethCommon.HexToAddress("0x1"),
		big.NewInt(10),
		uint64(100),
		big.NewInt(123),
		nil,
	)
	res := &types.Result{
		ValidationError:         nil,
		VMError:                 nil,
		TxType:                  tx.Type(),
		GasConsumed:             1,
		CumulativeGasUsed:       1,
		GasRefund:               0,
		DeployedContractAddress: &types.Address{0x5, 0x6, 0x7},
		ReturnedData:            []byte{0x55},
		Logs: []*gethTypes.Log{{
			Address: gethCommon.Address{0x1, 0x2},
			Topics:  []gethCommon.Hash{{0x5, 0x6}, {0x7, 0x8}},
		}, {
			Address: gethCommon.Address{0x3, 0x5},
			Topics:  []gethCommon.Hash{{0x2, 0x66}, {0x7, 0x1}},
		}},
		TxHash:                tx.Hash(),
		Index:                 txIndex,
		PrecompiledCalls:      []byte{},
		StateChangeCommitment: []byte{},
	}

	txEncoded, err := tx.MarshalBinary()
	if err != nil {
		return nil, nil, flow.Event{}, err
	}

	ev := events.NewTransactionEvent(
		res,
		txEncoded,
		1,
	)

	cdcEv, err := ev.Payload.ToCadence(flowGo.Previewnet)
	if err != nil {
		return nil, nil, flow.Event{}, err
	}

	flowEvent := flow.Event{
		Type:  string(ev.Etype),
		Value: cdcEv,
	}

	return TransactionCall{Transaction: tx}, res, flowEvent, err
}

func newBlock(height uint64, txHashes []gethCommon.Hash) (*Block, flow.Event, error) {
	gethBlock := types.NewBlock(
		gethCommon.HexToHash("0x01"),
		height,
		uint64(1337),
		big.NewInt(100),
		gethCommon.HexToHash("0x15"),
	)
	gethBlock.TransactionHashRoot = types.TransactionHashes(txHashes).RootHash()
	evmBlock := &Block{
		Block:             gethBlock,
		TransactionHashes: txHashes,
	}

	ev := events.NewBlockEvent(gethBlock)

	cadenceEvent, err := ev.Payload.ToCadence(flowGo.Previewnet)
	if err != nil {
		return nil, flow.Event{}, err
	}

	flowEvent := flow.Event{
		Type:  string(ev.Etype),
		Value: cadenceEvent,
	}

	return evmBlock, flowEvent, nil
}
