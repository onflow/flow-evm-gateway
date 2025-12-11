package config

import (
	"crypto/ecdsa"
	"io"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	flowGoKMS "github.com/onflow/flow-go-sdk/crypto/cloudkms"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/rs/zerolog"
)

const (
	// Default InitCadenceHeight for initializing the database on a local emulator.
	// TODO: temporary fix until https://github.com/onflow/flow-go/issues/5481 is
	// fixed upstream and released.
	EmulatorInitCadenceHeight = uint64(0)

	// Default InitCadenceHeight for initializing the database on a live network.
	// We don't use 0 as it has a special meaning to represent latest block in the AN API context.
	LiveNetworkInitCadenceHeight = uint64(1)

	// Testnet height at which the `EVM` system contract was first deployed.
	// This is the first height at which the EVM state starts.
	// Updated post-Forte hardfork for testnet52+ (previously 211176670 was outdated)
	TestnetInitCadenceHeight = uint64(218215349)

	// Mainnet height at which the `EVM` system contract was first deployed.
	// This is the first height at which the EVM state starts.
	// reference: https://github.com/onflow/flow/blob/9203b57a04d422360de257bcd92c522a2f51d3b0/sporks.json#L91
	MainnetInitCadenceHeight = uint64(85981135)
)

type TxStateValidation string

const (
	LocalIndexValidation = "local-index"
	TxSealValidation     = "tx-seal"
)

type Config struct {
	// DatabaseDir is where the database should be stored.
	DatabaseDir string
	// AccessNodeHost defines the current spork Flow network AN host.
	AccessNodeHost string
	// AccessNodePreviousSporkHosts contains a list of the ANs hosts for each spork
	AccessNodePreviousSporkHosts []string
	// GRPCPort for the RPC API server
	RPCPort int
	// GRPCHost for the RPC API server
	RPCHost string
	// WSEnabled determines if the websocket server is enabled.
	WSEnabled bool
	// EVMNetworkID provides the EVM chain ID.
	EVMNetworkID *big.Int
	// FlowNetworkID is the Flow network ID that the EVM is hosted on (mainnet, testnet, emulator...)
	FlowNetworkID flowGo.ChainID
	// Coinbase is EVM address that collects the EVM operator fees collected
	// when transactions are being submitted.
	Coinbase common.Address
	// COAAddress is Flow address that holds COA account used for submitting transactions.
	COAAddress flow.Address
	// COAKey is Flow key to the COA account. WARNING: do not use in production
	COAKey crypto.PrivateKey
	// COACloudKMSKey is a Cloud KMS key that will be used for signing transactions.
	COACloudKMSKey *flowGoKMS.Key
	// COATxLookupEnabled enables tracking of Cadence transactions to release COA signing
	// keys much faster. Increases capacity of the available COA signing keys for nodes
	// with high tx volume.
	COATxLookupEnabled bool
	// GasPrice is a fixed gas price that will be used when submitting transactions.
	GasPrice *big.Int
	// EnforceGasPrice defines whether the minimum gas price should be enforced.
	EnforceGasPrice bool
	// InitCadenceHeight is used for initializing the database on a local emulator or a live network.
	InitCadenceHeight uint64
	// LogLevel defines how verbose the output log is
	LogLevel zerolog.Level
	// LogWriter defines the writer used for logging
	LogWriter io.Writer
	// Logger if you bring your own
	Logger *zerolog.Logger
	// RateLimit requests made by the client identified by IP over any protocol (ws/http).
	RateLimit uint64
	// Address header used to identified clients, usually set by the proxy
	AddressHeader string
	// FilterExpiry defines the time it takes for an idle filter to expire
	FilterExpiry time.Duration
	// ForceStartCadenceHeight will force set the starting Cadence height, this should be only used for testing or locally.
	ForceStartCadenceHeight uint64
	// WalletEnabled sets whether wallet APIs are enabled
	WalletEnabled bool
	// WalletKey used for signing transactions
	WalletKey *ecdsa.PrivateKey
	// MetricsPort defines the port the metric server will listen to
	MetricsPort int
	// IndexOnly configures the gateway to not accept any transactions but only queries of the state
	IndexOnly bool
	// ProfilerEnabled sets whether the profiler server is enabled
	ProfilerEnabled bool
	// ProfilerHost is the host for the profiler server will listen to (e.g. localhost, 0.0.0.0)
	ProfilerHost string
	// ProfilerPort is the port for the profiler server
	ProfilerPort int
	// TxStateValidation sets the transaction validation mechanism. It can validate
	// using the local state index, or wait for the outer Flow transaction to seal.
	TxStateValidation string
	// TxRequestLimit is the number of transaction submissions to allow per interval.
	TxRequestLimit uint64
	// TxRequestLimitDuration is the time interval upon which to enforce transaction submission
	// rate limiting.
	TxRequestLimitDuration time.Duration
	// TxBatchMode configures the gateway to send transactions in batches grouped by EOA address,
	// to avoid the re-ordering issue for EOAs with a high-volume of transaction submission
	// in small intervals.
	TxBatchMode bool
	// TxBatchInterval is the time interval upon which to submit the transaction batches to the
	// Flow network.
	TxBatchInterval time.Duration
	// EOAActivityCacheTTL is the time interval used to track EOA activity. Tx send more
	// frequently than this interval will be batched.
	// Useful only when batch transaction submission is enabled.
	EOAActivityCacheTTL time.Duration
	// RpcRequestTimeout is the maximum duration at which JSON-RPC requests should generate
	// a response, before they timeout.
	RpcRequestTimeout time.Duration
	// ERC-4337 Configuration
	// EntryPointAddress is the address of the ERC-4337 EntryPoint contract
	// Use the official EntryPoint contract from eth-infinitism:
	// https://github.com/eth-infinitism/account-abstraction
	// Canonical v0.6 address (CREATE2): 0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789
	// Flow Testnet v0.9.0 address: 0xcf1e8398747a05a997e8c964e957e47209bdff08
	// See docs/FLOW_TESTNET_DEPLOYMENT.md for deployed contract addresses
	EntryPointAddress common.Address
	// EntryPointSimulationsAddress is the address of the EntryPointSimulations contract
	// For EntryPoint v0.7+, simulation methods (simulateValidation) were moved to a separate contract
	// Flow Testnet EntryPointSimulations: 0xfFDDAa4a9Ab363f02Ba26a5fc45Ec714562683D3
	// If not set, gateway will attempt to use EntryPoint address (for backwards compatibility with v0.6)
	EntryPointSimulationsAddress common.Address
	// BundlerEnabled enables ERC-4337 bundler functionality
	BundlerEnabled bool
	// MaxOpsPerBundle is the maximum number of UserOperations per EntryPoint.handleOps() call
	MaxOpsPerBundle int
	// UserOpTTL is the time to live for pending UserOperations in the pool
	UserOpTTL time.Duration
	// BundlerBeneficiary is the address that receives fees from EntryPoint execution
	BundlerBeneficiary common.Address
	// BundlerInterval is the interval at which the bundler checks for and processes pending UserOperations
	// Default: 800ms (0.8 seconds). Lower values reduce latency but increase RPC load on Access Node.
	BundlerInterval time.Duration
	// ERC-4337 Stake Requirements (EntryPoint v0.9.0)
	// These values enforce minimum stake requirements for UserOperation validation
	// Production values match Ethereum mainnet economics (~$3,300 for paymasters, ~$330 for senders)
	// Testnet values are lower for easier testing
	// MinSenderStake is the minimum stake required for sender accounts (in FLOW)
	// Production: 3,300 FLOW (~$330, equivalent to 0.1 ETH)
	// Testnet: 1,000 FLOW (~$100)
	MinSenderStake *big.Int
	// MinFactoryStake is the minimum stake required for factory contracts (in FLOW)
	// Production: 3,300 FLOW (~$330, equivalent to 0.1 ETH)
	// Testnet: 1,000 FLOW (~$100)
	MinFactoryStake *big.Int
	// MinPaymasterStake is the minimum stake required for paymaster contracts (in FLOW)
	// Production: 33,000 FLOW (~$3,300, equivalent to 1 ETH)
	// Testnet: 10,000 FLOW (~$1,000)
	MinPaymasterStake *big.Int
	// MinAggregatorStake is the minimum stake required for aggregator contracts (in FLOW)
	// Production: 33,000 FLOW (~$3,300, equivalent to 1 ETH)
	// Testnet: 10,000 FLOW (~$1,000)
	MinAggregatorStake *big.Int
	// MinUnstakeDelaySec is the minimum unstake delay required (in seconds)
	// This is typically 7 days (604800 seconds) for EntryPoint v0.9.0
	MinUnstakeDelaySec uint64
}

// SetDefaultStakeRequirements sets default stake requirements based on network type
// Production values match Ethereum mainnet economics (~$3,300 for paymasters, ~$330 for senders)
// Testnet values are lower for easier testing
func (c *Config) SetDefaultStakeRequirements() {
	// Default unstake delay: 7 days (604800 seconds)
	if c.MinUnstakeDelaySec == 0 {
		c.MinUnstakeDelaySec = 604800 // 7 days
	}

	// Determine if we're on testnet or production
	isTestnet := c.FlowNetworkID == flowGo.Testnet || c.FlowNetworkID == flowGo.Emulator || c.FlowNetworkID == flowGo.Previewnet

	if isTestnet {
		// Testnet values (for easier testing)
		if c.MinSenderStake == nil {
			c.MinSenderStake = big.NewInt(1_000) // 1,000 FLOW (~$100)
		}
		if c.MinFactoryStake == nil {
			c.MinFactoryStake = big.NewInt(1_000) // 1,000 FLOW (~$100)
		}
		if c.MinPaymasterStake == nil {
			c.MinPaymasterStake = big.NewInt(10_000) // 10,000 FLOW (~$1,000)
		}
		if c.MinAggregatorStake == nil {
			c.MinAggregatorStake = big.NewInt(10_000) // 10,000 FLOW (~$1,000)
		}
	} else {
		// Production values (matching Ethereum mainnet economics)
		if c.MinSenderStake == nil {
			c.MinSenderStake = big.NewInt(3_300) // 3,300 FLOW (~$330, equivalent to 0.1 ETH)
		}
		if c.MinFactoryStake == nil {
			c.MinFactoryStake = big.NewInt(3_300) // 3,300 FLOW (~$330, equivalent to 0.1 ETH)
		}
		if c.MinPaymasterStake == nil {
			c.MinPaymasterStake = big.NewInt(33_000) // 33,000 FLOW (~$3,300, equivalent to 1 ETH)
		}
		if c.MinAggregatorStake == nil {
			c.MinAggregatorStake = big.NewInt(33_000) // 33,000 FLOW (~$3,300, equivalent to 1 ETH)
		}
	}
}
