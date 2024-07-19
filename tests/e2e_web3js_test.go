package tests

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestWeb3_E2E(t *testing.T) {

	t.Run("test setup sanity check", func(t *testing.T) {
		runWeb3Test(t, "setup_test")
	})

	t.Run("test net JSON-RPC endpoints", func(t *testing.T) {
		runWeb3Test(t, "net_namespace_test")
	})

	t.Run("read-only interactions", func(t *testing.T) {
		runWeb3Test(t, "eth_non_interactive_test")
	})

	t.Run("deploy contract and call methods", func(t *testing.T) {
		runWeb3Test(t, "eth_deploy_contract_and_interact_test")
	})

	t.Run("deploy multicall3 contract and call methods", func(t *testing.T) {
		runWeb3Test(t, "eth_multicall3_contract_test")
	})

	t.Run("test fetch transaction", func(t *testing.T) {
		runWeb3Test(t, "eth_get_transaction_by_hash_test")
	})

	t.Run("store revertReason field in transaction receipts", func(t *testing.T) {
		runWeb3Test(t, "eth_revert_reason_test")
	})

	t.Run("transfer Flow between EOA accounts", func(t *testing.T) {
		runWeb3Test(t, "eth_transfer_between_eoa_accounts_test")
	})

	t.Run("logs emitting and filtering", func(t *testing.T) {
		runWeb3Test(t, "eth_logs_filtering_test")
	})

	t.Run("test get filter logs", func(t *testing.T) {
		runWeb3Test(t, "eth_get_filter_logs_test")
	})

	t.Run("rate-limit requests made by single client", func(t *testing.T) {
		runWeb3Test(t, "eth_rate_limit_test")
	})

	t.Run("handling of failures", func(t *testing.T) {
		runWeb3Test(t, "eth_failure_handling_test")
	})

	t.Run("batch run transactions", func(t *testing.T) {
		// create multiple value transfers and batch run them before the test
		runWeb3TestWithSetup(t, "eth_batch_retrieval_test", func(emu emulator.Emulator) {
			// crate test accounts
			senderKey, err := crypto.HexToECDSA("6a0eb450085e825dd41cc3dd85e4166d4afbb0162488a3d811a0637fa7656abf")
			require.NoError(t, err)
			receiver := common.HexToAddress("0xd0bA5bc19775c36faD888a9C856baffD6d575482")

			// create a value transfer transactions
			const batchSize = 10
			encodedTxs := make([]cadence.Value, batchSize)
			for i := range encodedTxs {
				transferPayload, _, err := evmSign(big.NewInt(0), 50000, senderKey, uint64(i), &receiver, nil)
				require.NoError(t, err)

				tx, err := cadence.NewString(hex.EncodeToString(transferPayload))
				require.NoError(t, err)

				encodedTxs[i] = tx
			}

			res, err := flowSendTransaction(
				emu,
				`transaction(encodedTxs: [String]) {
					prepare(signer: auth(Storage) &Account) {
						var txs: [[UInt8]] = []
						for enc in encodedTxs {
							txs.append(enc.decodeHex())
						}
						
						let txResults = EVM.batchRun(
							txs: txs,
							coinbase: EVM.EVMAddress(bytes: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19])
						)
						for txResult in txResults {
							assert(txResult.status == EVM.Status.successful, message: "failed to execute tx, error: ".concat(txResult.errorCode.toString()))
						}
					}
				}`,
				cadence.NewArray(encodedTxs),
			)
			require.NoError(t, err)
			require.NoError(t, res.Error)
		})
	})

	t.Run("streaming of entities and subscription", func(t *testing.T) {
		runWeb3Test(t, "eth_streaming_test")
	})

	t.Run("streaming of entities and subscription with filters", func(t *testing.T) {
		runWeb3Test(t, "eth_streaming_filters_test")
	})
}
