package tests

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/emulator"
	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/testutils"
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

	t.Run("verify Cadence arch calls", func(t *testing.T) {
		runWeb3Test(t, "verify_cadence_arch_calls_test")
	})

	t.Run("test transaction traces", func(t *testing.T) {
		runWeb3Test(t, "debug_traces_test")
	})

	t.Run("test debug utils", func(t *testing.T) {
		runWeb3Test(t, "debug_util_test")
	})

	t.Run("test contract call overrides", func(t *testing.T) {
		runWeb3Test(t, "contract_call_overrides_test")
	})

	t.Run("test gas estimation block overrides", func(t *testing.T) {
		runWeb3Test(t, "estimate_gas_overrides_test")
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

	t.Run("apply state overrides", func(t *testing.T) {
		runWeb3Test(t, "eth_state_overrides_test")
	})

	t.Run("test retrieval of contract storage slots", func(t *testing.T) {
		runWeb3Test(t, "eth_get_storage_at_test")
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
		runWeb3TestWithSetup(t, "eth_logs_filtering_test", func(emu emulator.Emulator) {
			// Run an arbitrary transaction, to form an empty EVM block
			// through the system chunk transaction. This is needed
			// to emulate the `eth_getLogs` by passing the block hash
			// of a block without EVM transactions/receipts.
			res, err := flowSendTransaction(
				emu,
				`transaction() {
					prepare(signer: auth(Storage) &Account) {
						let currentBlock = getCurrentBlock()
						assert(currentBlock.height > 0, message: "current block is zero")
					}
				}`,
			)
			require.NoError(t, err)
			require.NoError(t, res.Error)
		})
	})

	t.Run("gas price updated with surge factor multipler", func(t *testing.T) {
		runWeb3TestWithSetup(t, "eth_gas_price_surge_test", func(emu emulator.Emulator) {
			surgeFactorValues := []string{"1.1", "2.0", "4.0", "10.0", "100.0"}
			for _, surgeFactor := range surgeFactorValues {
				res, err := flowSendTransaction(
					emu,
					fmt.Sprintf(
						`
							import FlowFees from 0xe5a8b7f23e8b548f

							// This transaction sets the FlowFees parameters
							transaction() {
								let flowFeesAccountAdmin: &FlowFees.Administrator

								prepare(signer: auth(BorrowValue) &Account) {
									self.flowFeesAccountAdmin = signer.storage.borrow<&FlowFees.Administrator>(
										from: /storage/flowFeesAdmin
									) ?? panic("Unable to borrow reference to administrator resource")
								}

								execute {
									self.flowFeesAccountAdmin.setFeeParameters(
										surgeFactor: %s,
										inclusionEffortCost: 1.0,
										executionEffortCost: 1.0
									)
								}
							}

						`,
						surgeFactor,
					),
				)
				require.NoError(t, err)
				require.NoError(t, res.Error)
			}
		})
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
			deployPayload, _, err := evmSign(big.NewInt(0), 1_250_000, accountKey, nonce, nil, contractCode)
			require.NoError(t, err)
			nonce += 1

			const batchSize = 6
			payloads := make([][]byte, batchSize)
			payloads[0] = deployPayload
			contractAddress := common.HexToAddress("99a64c993965f8d69f985b5171bc20065cc32fab")
			// contract call transactions, these emit logs/events
			for i := int64(1); i <= 5; i++ {
				callData := storageContract.MakeCallData(t, "sum", big.NewInt(i*10), big.NewInt(i*20))
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

	t.Run("test EVM.dryCall & COA.dryCall", func(t *testing.T) {
		runWeb3TestWithSetup(t, "evm_dry_call_test", func(emu emulator.Emulator) {
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
			deployPayload, _, err := evmSign(big.NewInt(0), 1_250_000, accountKey, nonce, nil, contractCode)
			require.NoError(t, err)
			nonce += 1

			// contract call transaction (store)
			contractAddress := common.HexToAddress("99a64c993965f8d69f985b5171bc20065cc32fab")
			callData := storageContract.MakeCallData(t, "storeWithLog", big.NewInt(42))
			callPayload, _, err := evmSign(big.NewInt(0), 55_000, accountKey, nonce, &contractAddress, callData)
			require.NoError(t, err)
			nonce += 1
			payloads := [][]byte{deployPayload, callPayload}

			const batchSize = 2
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
							assert(
								txResult.status == EVM.Status.successful,
								message: txResult.errorCode.toString()
							)
						}

						var callResult = EVM.dryCall(
							from: EVM.addressFromString("0xFACF71692421039876a5BB4F10EF7A439D8ef61E"),
							to: EVM.addressFromString("0x99a64c993965f8d69f985b5171bc20065cc32fab"),
							data: EVM.encodeABIWithSignature("storeWithLog(uint256)", [UInt256(1453)]),
							gasLimit: 30000,
							value: EVM.Balance(attoflow: 0)
						)
						assert(callResult.status == EVM.Status.successful)

						callResult = EVM.dryCall(
							from: EVM.addressFromString("0xFACF71692421039876a5BB4F10EF7A439D8ef61E"),
							to: EVM.addressFromString("0x99a64c993965f8d69f985b5171bc20065cc32fab"),
							data: EVM.encodeABIWithSignature("retrieve()", []),
							gasLimit: 30000,
							value: EVM.Balance(attoflow: 0)
						)
						assert(callResult.status == EVM.Status.successful)
						var returnData = EVM.decodeABI(types: [Type<UInt256>()], data: callResult.data)
						// assert that the above EVM.dryCall with storeWithLog(1453), had no
						// effect on the contract's state.
						assert(returnData[0] as! UInt256 == 42)

						let coa <- EVM.createCadenceOwnedAccount()
						callResult = coa.dryCall(
							to: EVM.addressFromString("0x99a64c993965f8d69f985b5171bc20065cc32fab"),
							data: EVM.encodeABIWithSignature("storeWithLog(uint256)", [UInt256(1515)]),
							gasLimit: 30000,
							value: EVM.Balance(attoflow: 0)
						)
						assert(callResult.status == EVM.Status.successful)

						callResult = coa.dryCall(
							to: EVM.addressFromString("0x99a64c993965f8d69f985b5171bc20065cc32fab"),
							data: EVM.encodeABIWithSignature("retrieve()", []),
							gasLimit: 30000,
							value: EVM.Balance(attoflow: 0)
						)
						assert(callResult.status == EVM.Status.successful)
						returnData = EVM.decodeABI(types: [Type<UInt256>()], data: callResult.data)
						// assert that the above coa.dryCall with storeWithLog(1515), had no
						// effect on the contract's state.
						assert(returnData[0] as! UInt256 == 42)

						destroy <- coa
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

	t.Run("test EIP-7720 contract writes", func(t *testing.T) {
		runWeb3Test(t, "eth_eip_7702_contract_write_test")
	})

	t.Run("test EIP-7720 sending transactions", func(t *testing.T) {
		runWeb3Test(t, "eth_eip_7702_sending_transactions_test")
	})

	t.Run("test pre-Pectra changes", func(t *testing.T) {
		// set the Prague hard-fork activation to 24 hours from now
		evmEmulator.PreviewnetPragueActivation = uint64(time.Now().Add(24 * time.Hour).Unix())
		defer func() {
			// set it back to its original value
			evmEmulator.PreviewnetPragueActivation = uint64(0)
		}()

		runWeb3Test(t, "eth_pectra_upgrade_test")
	})
}
