package tests

import (
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-emulator/emulator"
)

func TestWeb3_E2E(t *testing.T) {

	t.Run("test setup sanity check", func(t *testing.T) {
		runWeb3Test(t, "setup_test")
	})

	t.Run("read-only interactions", func(t *testing.T) {
		runWeb3Test(t, "eth_non_interactive_test")
	})

	t.Run("deploy contract and call methods", func(t *testing.T) {
		runWeb3Test(t, "eth_deploy_contract_and_interact_test")
	})

	t.Run("transfer Flow between EOA accounts", func(t *testing.T) {
		runWeb3Test(t, "eth_transfer_between_eoa_accounts_test")
	})

	t.Run("logs emitting and filtering", func(t *testing.T) {
		runWeb3Test(t, "eth_logs_filtering_test")
	})

	t.Run("eth_getFilterLogs", func(t *testing.T) {
		runWeb3Test(t, "eth_get_filter_logs_test")
	})

	t.Run("streaming of entities and subscription", func(t *testing.T) {
		runWeb3Test(t, "eth_streaming_test")
	})

	t.Run("streaming of entities and subscription with filters", func(t *testing.T) {
		runWeb3Test(t, "eth_streaming_filters_test")
	})

	t.Run("batch run transactions", func(t *testing.T) {
		runWeb3TestWithSetup(t, "eth_batch_retrieval_test", func(emu emulator.Emulator) error {
			tx1, err := cadence.NewString("f9015880808301e8488080b901086060604052341561000f57600080fd5b60eb8061001d6000396000f300606060405260043610603f576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063c6888fa1146044575b600080fd5b3415604e57600080fd5b606260048080359060200190919050506078565b6040518082815260200191505060405180910390f35b60007f24abdb5865df5079dcc5ac590ff6f01d5c16edbc5fab4e195d9febd1114503da600783026040518082815260200191505060405180910390a16007820290509190505600a165627a7a7230582040383f19d9f65246752244189b02f56e8d0980ed44e7a56c0b200458caad20bb002982052fa09c05a7389284dc02b356ec7dee8a023c5efd3a9d844fa3c481882684b0640866a057e96d0a71a857ed509bb2b7333e78b2408574b8cc7f51238f25c58812662653")
			if err != nil {
				return err
			}
			tx2, err := cadence.NewString("f885018082c3509499466ed2e37b892a2ee3e9cd55a98b68f5735db280a4c6888fa10000000000000000000000000000000000000000000000000000000000000006820530a03547bcd56e6c6103e78c8c3b34f480108f66ad37282d887033b8c5951f0c70a0a00f5136f6033244a265e1ebaf48cf83d4fdf13f53b468d8fd924c9deb1537dd8d")
			if err != nil {
				return err
			}

			res, err := flowSendTransaction(
				emu,
				`transaction(tx1: String, tx2: String) {
					prepare(signer: auth(Storage) &Account) {
						let txs: [[UInt8]] = [tx1.decodeHex(), tx2.decodeHex()]
						let txResults = EVM.batchRun(
							txs: txs,
							coinbase: EVM.EVMAddress(bytes: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19])
						)
						for txResult in txResults {
							assert(txResult.status == EVM.Status.successful, message: "failed to execute tx")
						}
					}
				}`,
				tx1,
				tx2,
			)
			if err != nil {
				return err
			}
			if res.Error != nil {
				return res.Error
			}
			return nil
		})
	})
}
