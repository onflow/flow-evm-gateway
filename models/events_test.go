package models

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

func TestCadenceEvents_Block(t *testing.T) {

	evmBlock := types.NewBlock(common.HexToHash("0x01"), 1, big.NewInt(100), common.HexToHash("0x02"), nil)
	ev := types.NewBlockExecutedEvent(evmBlock)
	cadenceEvent, err := ev.Payload.CadenceEvent()
	require.NoError(t, err)

	v, _ := cadence.NewString("invalid")
	invalid, _ := cadence.NewValue(v)

	tests := []struct {
		name   string
		events flow.BlockEvents
		block  *types.Block
		err    error
	}{
		{
			name: "BlockExecutedEventExists",
			events: flow.BlockEvents{Events: []flow.Event{
				{
					Type:             string(ev.Etype),
					TransactionIndex: 0,
					EventIndex:       0,
					Value:            cadenceEvent,
				},
			}},
			block: evmBlock,
		}, {
			name:   "BlockExecutedEventEmpty",
			events: flow.BlockEvents{Events: []flow.Event{}},
			block:  nil,
		}, {
			name: "BlockExecutedNotFound",
			events: flow.BlockEvents{Events: []flow.Event{{
				Type:             string(ev.Etype),
				TransactionIndex: 0,
				EventIndex:       0,
				Value:            cadence.NewEvent([]cadence.Value{invalid}),
			}}},
			block: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCadenceEvents(tt.events)
			b, err := c.Block()
			require.NoError(t, err)

			if tt.block != nil {
				ttHash, err := tt.block.Hash()
				require.NoError(t, err)
				hash, err := b.Hash()
				require.NoError(t, err)
				assert.Equal(t, ttHash, hash)
			} else {
				assert.Nil(t, b)
			}
		})
	}
}
