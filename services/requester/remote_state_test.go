package requester

import (
	"context"
	"encoding/hex"
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

	stateDB, err := state.NewStateDB(ledger, previewnetStorageAddress)

	assert.NotEmpty(t, stateDB.GetCode(testAddress))
	assert.NotEmpty(t, stateDB.GetNonce(testAddress))
	assert.Empty(t, stateDB.GetBalance(testAddress))
	assert.NotEmpty(t, stateDB.GetCodeSize(testAddress))
	assert.NotEmpty(t, stateDB.GetState(testAddress, gethCommon.Hash{}))
}

/*
Testing from local machine (bottleneck is network delay to previewnet AN)

Benchmark_RemoteLedger_GetBalance-8   	       9	1144204361 ns/op
*/
func Benchmark_RemoteLedger_GetBalance(b *testing.B) {
	const previewnetHost = "access-001.previewnet1.nodes.onflow.org:9000"
	cadenceHeight, err := getPreviewnetLatestHeight(previewnetHost)
	require.NoError(b, err)

	// we have to include ledger creation since the loading of the collection
	// will be done only once per height, all the subsequent requests for
	// getting the balance will work on already loaded state and thus be fast
	for i := 0; i < b.N; i++ {
		ledger, err := newRemoteLedger(previewnetHost, cadenceHeight)
		require.NoError(b, err)

		stateDB, err := state.NewStateDB(ledger, previewnetStorageAddress)
		require.NoError(b, err)

		addrBytes, err := hex.DecodeString("BC9985a24c0846cbEdd6249868020A84Df83Ea85")
		require.NoError(b, err)
		testAddress := types.NewAddressFromBytes(addrBytes).ToCommon()

		assert.Empty(b, stateDB.GetBalance(testAddress))
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