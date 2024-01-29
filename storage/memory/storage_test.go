package memory

import (
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/stretchr/testify/suite"
	"testing"
)

// tests that make sure the implementation conform to the interface expected behaviour
func TestStorageSuite(t *testing.T) {
	suite.Run(t, &storage.BlockTestSuite{Blocks: NewBlockStorage()})
}
