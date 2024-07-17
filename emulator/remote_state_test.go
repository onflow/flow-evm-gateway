package emulator

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GetCode(t *testing.T) {
	ledger, err := newRemoteLedger("access-001.previewnet1.nodes.onflow.org:9000", uint64(0))
	require.NoError(t, err)

	emu := newEmulator(ledger)
	view, err := emu.view()
	require.NoError(t, err)

	addrBytes, err := hex.DecodeString("BC9985a24c0846cbEdd6249868020A84Df83Ea85")
	require.NoError(t, err)
	address := types.NewAddressFromBytes(addrBytes)

	code, err := view.CodeOf(address)
	require.NoError(t, err)
	assert.NotEmpty(t, code)
	fmt.Println(fmt.Sprintf("%x", code))

	balance, err := view.BalanceOf(address)
	require.NoError(t, err)
	assert.NotEmpty(t, balance)

	nonce, err := view.NonceOf(address)
	require.NoError(t, err)
	assert.NotEmpty(t, nonce)

	storageAddress := flow.HexToAddress("0x4f6fd534ddd3fc5f")
	stateDB, err := state.NewStateDB(ledger, storageAddress)

	code2 := stateDB.GetCode(address.ToCommon())
	assert.NotEmpty(t, code2)

	hash := stateDB.GetState(address.ToCommon(), gethCommon.BytesToHash(address.Bytes()))
	assert.NotEmpty(t, hash)
	require.Equal(t, code, hash)
}
