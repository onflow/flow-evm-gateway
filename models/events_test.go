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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCadenceEvents_Block(t *testing.T) {
	invalid := cadence.String("invalid")

	b0, e0, err := newBlock(0)
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
}

func newBlock(height uint64) (*Block, flow.Event, error) {
	gethBlock := types.NewBlock(gethCommon.HexToHash("0x01"), height, uint64(1337), big.NewInt(100))
	evmBlock := &Block{Block: gethBlock}
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
