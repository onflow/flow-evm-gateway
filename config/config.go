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
	TestnetInitCadenceHeight = uint64(211176670)

	// Mainnet height at which the `EVM` system contract was first deployed.
	// This is the first height at which the EVM state starts.
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
}
