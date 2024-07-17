package emulator

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/onflow/flow-go/fvm/evm/emulator/state"
	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/onflow/flow/protobuf/go/flow/access"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Test_E2E_Previewnet_RemoteLedger(t *testing.T) {
	ledger, err := newPreviewnetLedger()
	require.NoError(t, err)

	// this is a pre-established test account on previewnet
	addrBytes, err := hex.DecodeString("BC9985a24c0846cbEdd6249868020A84Df83Ea85")
	require.NoError(t, err)
	testAddress := types.NewAddressFromBytes(addrBytes).ToCommon()

	stateDB, err := state.NewStateDB(ledger, previewnetStorage)

	assert.NotEmpty(t, stateDB.GetCode(testAddress))
	assert.NotEmpty(t, stateDB.GetNonce(testAddress))
	assert.NotEmpty(t, stateDB.GetBalance(testAddress))
	assert.NotEmpty(t, stateDB.GetCodeSize(testAddress))
	assert.NotEmpty(t, stateDB.GetState(testAddress, gethCommon.Hash{}))
}

func Benchmark_RemoteLedger_GetBalance(b *testing.B) {
	ledger, err := newPreviewnetLedger()
	require.NoError(b, err)

	stateDB, err := state.NewStateDB(ledger, previewnetStorage)

	// we generate a big random list of addresses, so we avoid any caching in benchmarks
	const count = 80
	addresses := make([]gethCommon.Address, count)
	for i := range addresses {
		addrBytes, err := hex.DecodeString(fmt.Sprintf("BC9985a24c0846cbEdd6249868020A84Df83Ea%d", i+10))
		require.NoError(b, err)
		addresses[i] = types.NewAddressFromBytes(addrBytes).ToCommon()
	}

	for i := 0; i < b.N; i++ {
		assert.Empty(b, stateDB.GetBalance(addresses[b.N%count]))
	}
}

func newPreviewnetLedger() (*remoteLedger, error) {
	const previewnetHost = "access-001.previewnet1.nodes.onflow.org:9000"

	cadenceHeight, err := getPreviewnetLatestHeight(previewnetHost)
	if err != nil {
		return nil, err
	}

	return newRemoteLedger(previewnetHost, cadenceHeight)
}

func getPreviewnetLatestHeight(host string) (uint64, error) {
	conn, err := grpc.Dial(
		host,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024*1024*1024)),
	)
	if err != nil {
		return 0, err
	}
	client := access.NewAccessAPIClient(conn)
	res, err := client.GetLatestBlockHeader(context.Background(), &access.GetLatestBlockHeaderRequest{IsSealed: true})
	if err != nil {
		return 0, err
	}

	return res.Block.Height, nil
}
