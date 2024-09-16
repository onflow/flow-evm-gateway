package tests

import (
	_ "embed"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/emulator"
	"github.com/onflow/flow-go/fvm/evm/testutils"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var (
	//go:embed fixtures/storage.byte
	storageByteCode string

	//go:embed fixtures/storageABI.json
	storageABI string
)

func TestWeb3_E2E(t *testing.T) {

	t.Run("build EVM state", func(t *testing.T) {
		runWeb3Test(t, "build_evm_state_test")
	})

	t.Run("test cadence arch and environment calls", func(t *testing.T) {
		runWeb3Test(t, "cadence_arch_env_test")
	})

	t.Run("test setup sanity check", func(t *testing.T) {
		runWeb3Test(t, "setup_test")
	})

	t.Run("test blockchain is linked via parent hashes", func(t *testing.T) {
		runWeb3Test(t, "blockchain_test")
	})

	t.Run("test net JSON-RPC endpoints", func(t *testing.T) {
		runWeb3Test(t, "net_namespace_test")
	})

	t.Run("read-only interactions", func(t *testing.T) {
		runWeb3Test(t, "eth_non_interactive_test")
	})

	t.Run("test transaction type fees", func(t *testing.T) {
		runWeb3Test(t, "eth_transaction_type_fees_test")
	})

	t.Run("retrieve syncing status", func(t *testing.T) {
		runWeb3Test(t, "eth_syncing_status_test")
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

	t.Run("test filter-related endpoints", func(t *testing.T) {
		runWeb3Test(t, "eth_filter_endpoints_test")
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

	t.Run("batch run transactions with logs", func(t *testing.T) {
		// create multiple transactions that emit logs and batch run them before the test
		runWeb3TestWithSetup(t, "eth_batch_tx_logs_test", func(emu emulator.Emulator) {
			contractCode, err := hex.DecodeString(storageByteCode)
			require.NoError(t, err)
			storageContract := testutils.TestContract{
				ByteCode: contractCode,
				ABI:      storageABI,
			}

			// create test account for contract deployment and contract calls
			nonce := uint64(0)
			accountKey, err := crypto.HexToECDSA("f6d5333177711e562cabf1f311916196ee6ffc2a07966d9d4628094073bd5442")
			require.NoError(t, err)

			// contract deployment transaction
			deployPayload, _, err := evmSign(big.NewInt(0), 1_550_000, accountKey, nonce, nil, contractCode)
			require.NoError(t, err)
			nonce += 1

			const batchSize = 6
			payloads := make([][]byte, batchSize)
			payloads[0] = deployPayload
			contractAddress := common.HexToAddress("99a64c993965f8d69f985b5171bc20065cc32fab")
			// contract call transactions, these emit logs/events
			for i := 1; i <= 5; i++ {
				callData := storageContract.MakeCallData(t, "sum", big.NewInt(10), big.NewInt(20))
				callPayload, _, err := evmSign(big.NewInt(0), 55_000, accountKey, nonce, &contractAddress, callData)
				require.NoError(t, err)
				payloads[i] = callPayload
				nonce += 1
			}
			encodedTxs := make([]cadence.Value, batchSize)
			for i := range encodedTxs {
				tx, err := cadence.NewString(hex.EncodeToString(payloads[i]))
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
