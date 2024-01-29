package memory

import (
	"github.com/onflow/flow-evm-gateway/storage"
	"github.com/stretchr/testify/suite"
	"testing"
)

func TestStorageSuite(t *testing.T) {
	suite.Run(t, &storage.BlockTestSuite{Blocks: NewBlockStorage()})
}
