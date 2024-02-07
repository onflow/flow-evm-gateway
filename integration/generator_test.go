package integration

import (
	"context"
	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/cadence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
	"time"
)

var EOAAddress = common.HexToAddress("FACF71692421039876a5BB4F10EF7A439D8ef61E")

const rawPrivateKey = "f6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442"

func TestIntegration_SimpleTransactions(t *testing.T) {
	srv, err := startEmulator()
	require.NoError(t, err)

	blocks, _, _, err := startEventIngestionEngine(context.Background())
	require.NoError(t, err)

	emu := srv.Emulator()

	flowAmount, _ := cadence.NewUFix64("5.0")
	evmAmount := big.NewInt(int64((flowAmount.ToGoValue()).(uint64)))
	// todo use types.NewBalanceFromUFix64(evmAmount) when flow-go updated
	weiAmount := evmAmount.Mul(evmAmount, toWei)

	res, err := fundEOA(emu, flowAmount, EOAAddress)
	require.NoError(t, err)
	require.NoError(t, res.Error)

	assert.Len(t, res.Events, 6) // 4 evm events + 2 cadence events

	time.Sleep(3 * time.Second) // todo change

	// block 1 and 2 should be indexed
	blk, err := blocks.GetByHeight(1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), blk.Height)
	assert.Equal(t, weiAmount, blk.TotalSupply)
	require.Len(t, blk.TransactionHashes, 1)

	blk, err = blocks.GetByHeight(2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), blk.Height)
	assert.Equal(t, weiAmount, blk.TotalSupply)
	require.Len(t, blk.TransactionHashes, 1)

}
