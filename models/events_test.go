package models

import (
	"math/big"
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCadenceEvents_Block(t *testing.T) {

	v, _ := cadence.NewString("invalid")
	invalid, _ := cadence.NewValue(v)

	b0, e0, err := newBlock(0)
	require.NoError(t, err)

	b1, e1, err := newBlock(1)
	require.NoError(t, err)

	b2, e2, err := newBlock(2)
	require.NoError(t, err)

	tests := []struct {
		name   string
		events flow.BlockEvents
		blocks []*types.Block
		err    error
	}{
		{
			name:   "BlockExecutedEventExists",
			events: flow.BlockEvents{Events: []flow.Event{e0}},
			blocks: []*types.Block{b0},
		}, {
			name:   "BlockExecutedEventEmpty",
			events: flow.BlockEvents{Events: []flow.Event{}},
			blocks: []*types.Block{},
		}, {
			name: "BlockExecutedNotFound",
			events: flow.BlockEvents{Events: []flow.Event{{
				Type:  e0.Type,
				Value: cadence.NewEvent([]cadence.Value{invalid}),
			}}},
			blocks: []*types.Block{},
		}, {
			name:   "BlockExecutedOutOfOrder",
			events: flow.BlockEvents{Events: []flow.Event{e0, e2, e1}},
			blocks: []*types.Block{b0, b1, b2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCadenceEvents(tt.events)
			blocks, err := c.Blocks()
			require.NoError(t, err)

			if tt.blocks != nil {
				for i, ttb := range tt.blocks {
					ttHash, err := ttb.Hash()
					require.NoError(t, err)
					hash, err := blocks[i].Hash()
					require.NoError(t, err)
					assert.Equal(t, ttHash, hash)
				}
			} else {
				assert.Nil(t, blocks)
			}
		})
	}
}

func newBlock(height uint64) (*types.Block, flow.Event, error) {
	evmBlock := types.NewBlock(common.HexToHash("0x01"), height, big.NewInt(100), common.HexToHash("0x02"), nil)
	ev := types.NewBlockExecutedEvent(evmBlock)
	cadenceEvent, err := ev.Payload.CadenceEvent()
	if err != nil {
		return nil, flow.Event{}, err
	}

	flowEvent := flow.Event{
		Type:  string(ev.Etype),
		Value: cadenceEvent,
	}

	return evmBlock, flowEvent, nil
}
