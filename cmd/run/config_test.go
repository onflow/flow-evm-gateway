package run

import (
	"math/big"
	"testing"

	"github.com/onflow/flow-go/fvm/evm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-evm-gateway/config"
)

func TestParseConfig_EVMNetworkID(t *testing.T) {
	tests := []struct {
		name           string
		flowNetwork    string
		expectedChainID *big.Int
		shouldError     bool
	}{
		{
			name:           "flow-testnet sets EVMNetworkID to 545",
			flowNetwork:    "flow-testnet",
			expectedChainID: big.NewInt(545),
			shouldError:     false,
		},
		{
			name:           "flow-mainnet sets EVMNetworkID to 747",
			flowNetwork:    "flow-mainnet",
			expectedChainID: big.NewInt(747),
			shouldError:     false,
		},
		{
			name:           "flow-previewnet sets EVMNetworkID",
			flowNetwork:    "flow-previewnet",
			expectedChainID: types.FlowEVMPreviewNetChainID,
			shouldError:     false,
		},
		{
			name:           "flow-emulator sets EVMNetworkID",
			flowNetwork:    "flow-emulator",
			expectedChainID: types.FlowEVMPreviewNetChainID,
			shouldError:     false,
		},
		{
			name:        "invalid flow-network-id returns error",
			flowNetwork: "invalid-network",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global cfg
			cfg = config.Config{
				IndexOnly: true, // Set IndexOnly to skip COA key requirements
			}
			flowNetwork = tt.flowNetwork
			coinbase = "0x1234567890123456789012345678901234567890"
			coa = "0x01"
			gas = "1"
			key = ""
			keyAlg = "ECDSA_P256"
			logLevel = "info"
			logWriter = "stderr"
			filterExpiry = "1h"
			accessSporkHosts = ""
			initHeight = 0
			forceStartHeight = 0
			txStateValidation = "local-index"
			entryPointAddress = ""
			entryPointSimulationsAddress = ""
			bundlerBeneficiary = ""
			walletKey = ""

			err := parseConfigFromFlags()

			if tt.shouldError {
				require.Error(t, err, "expected error for invalid flow-network-id")
				return
			}

			require.NoError(t, err, "parseConfigFromFlags should not error for valid flow-network-id")

			// Verify EVMNetworkID is set and not nil
			require.NotNil(t, cfg.EVMNetworkID, "EVMNetworkID should not be nil")
			assert.NotEqual(t, 0, cfg.EVMNetworkID.Sign(), "EVMNetworkID should not be zero")

			// Verify it matches expected value
			if tt.expectedChainID != nil {
				assert.Equal(t, 0, cfg.EVMNetworkID.Cmp(tt.expectedChainID),
					"EVMNetworkID should match expected value. Got: %s, Expected: %s",
					cfg.EVMNetworkID.String(), tt.expectedChainID.String())
			}

			// For testnet and mainnet, verify exact values
			if tt.flowNetwork == "flow-testnet" {
				assert.Equal(t, 0, cfg.EVMNetworkID.Cmp(big.NewInt(545)),
					"flow-testnet should set EVMNetworkID to 545, got: %s", cfg.EVMNetworkID.String())
			}
			if tt.flowNetwork == "flow-mainnet" {
				assert.Equal(t, 0, cfg.EVMNetworkID.Cmp(big.NewInt(747)),
					"flow-mainnet should set EVMNetworkID to 747, got: %s", cfg.EVMNetworkID.String())
			}
		})
	}
}

func TestParseConfig_EVMNetworkID_Validation(t *testing.T) {
	t.Run("validates EVMNetworkID is not nil after parsing", func(t *testing.T) {
		// This test ensures our validation catches if the flow-go constants are nil
		cfg = config.Config{
			IndexOnly: true, // Set IndexOnly to skip COA key requirements
		}
		flowNetwork = "flow-testnet"
		coinbase = "0x1234567890123456789012345678901234567890"
		coa = "0x01"
		gas = "1"
		key = ""
		keyAlg = "ECDSA_P256"
		logLevel = "info"
		logWriter = "stderr"
		filterExpiry = "1h"
		accessSporkHosts = ""
		initHeight = 0
		forceStartHeight = 0
		txStateValidation = "local-index"
		entryPointAddress = ""
		entryPointSimulationsAddress = ""
		bundlerBeneficiary = ""
		walletKey = ""

		err := parseConfigFromFlags()
		require.NoError(t, err)

		// Verify the constants from flow-go are not nil
		assert.NotNil(t, types.FlowEVMTestNetChainID, "FlowEVMTestNetChainID constant should not be nil")
		assert.NotNil(t, types.FlowEVMMainNetChainID, "FlowEVMMainNetChainID constant should not be nil")
		assert.NotNil(t, types.FlowEVMPreviewNetChainID, "FlowEVMPreviewNetChainID constant should not be nil")

		// Verify they're not zero
		assert.NotEqual(t, 0, types.FlowEVMTestNetChainID.Sign(), "FlowEVMTestNetChainID should not be zero")
		assert.NotEqual(t, 0, types.FlowEVMMainNetChainID.Sign(), "FlowEVMMainNetChainID should not be zero")
		assert.NotEqual(t, 0, types.FlowEVMPreviewNetChainID.Sign(), "FlowEVMPreviewNetChainID should not be zero")

		// Verify exact values
		assert.Equal(t, 0, types.FlowEVMTestNetChainID.Cmp(big.NewInt(545)),
			"FlowEVMTestNetChainID should be 545, got: %s", types.FlowEVMTestNetChainID.String())
		assert.Equal(t, 0, types.FlowEVMMainNetChainID.Cmp(big.NewInt(747)),
			"FlowEVMMainNetChainID should be 747, got: %s", types.FlowEVMMainNetChainID.String())
	})
}

