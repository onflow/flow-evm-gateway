package memory

import (
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/stretchr/testify/suite"
	"testing"
)

// tests that make sure the implementation conform to the interface expected behaviour
func TestBlocks(t *testing.T) {
	suite.Run(t, &storage.BlockTestSuite{Blocks: NewBlockStorage()})
}

func TestReceipts(t *testing.T) {
	suite.Run(t, &storage.ReceiptTestSuite{ReceiptIndexer: NewReceiptStorage()})
}

func TestTransactions(t *testing.T) {
	suite.Run(t, &storage.TransactionTestSuite{TransactionIndexer: NewTransactionStorage()})
}
