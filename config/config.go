package config

import (
	"crypto/ecdsa"
	"io"
	"math/big"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
	flowGoKMS "github.com/onflow/flow-go-sdk/crypto/cloudkms"
	flowGo "github.com/onflow/flow-go/model/flow"
	"github.com/onflow/go-ethereum/common"
	"github.com/rs/zerolog"
)

// Default InitCadenceHeight for initializing the database on a local emulator.
// TODO: temporary fix until https://github.com/onflow/flow-go/issues/5481 is
// fixed upstream and released.
const EmulatorInitCadenceHeight = uint64(0)

// Default InitCadenceHeight for initializing the database on a live network.
// We don't use 0 as it has a special meaning to represent latest block in the AN API context.
const LiveNetworkInitCadenceHeght = uint64(1)

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
	// COAKeys is a slice of all the keys that will be used in key-rotation mechanism.
	COAKeys []crypto.PrivateKey
	// COACloudKMSKeys is a slice of all the keys and their versions that will be used in Cloud KMS key-rotation mechanism.
	COACloudKMSKeys []flowGoKMS.Key
	// CreateCOAResource indicates if the COA resource should be auto-created on
	// startup if one doesn't exist in the COA Flow address account
	CreateCOAResource bool
	// GasPrice is a fixed gas price that will be used when submitting transactions.
	GasPrice *big.Int
	// InitCadenceHeight is used for initializing the database on a local emulator or a live network.
	InitCadenceHeight uint64
	// LogLevel defines how verbose the output log is
	LogLevel zerolog.Level
	// LogWriter defines the writer used for logging
	LogWriter io.Writer
	// RateLimit requests made by the client identified by IP over any protocol (ws/http).
	RateLimit uint64
	// Address header used to identified clients, usually set by the proxy
	AddressHeader string
	// StreamLimit rate-limits the events sent to the client within 1 second time interval.
	StreamLimit float64
	// StreamTimeout defines the timeout the server waits for the event to be sent to the client.
	StreamTimeout time.Duration
	// FilterExpiry defines the time it takes for an idle filter to expire
	FilterExpiry time.Duration
	// ForceStartCadenceHeight will force set the starting Cadence height, this should be only used for testing or locally.
	ForceStartCadenceHeight uint64
	// HeartbeatInterval sets custom heartbeat interval for events
	HeartbeatInterval uint64
	// TracesBucketName sets the GCP bucket name where transaction traces are being stored.
	TracesBucketName string
	// TracesEnabled sets whether the node is supporting transaction traces.
	TracesEnabled bool
	// TracesBackfillStartHeight sets the starting block height for backfilling missing traces.
	TracesBackfillStartHeight uint64
	// TracesBackfillEndHeight sets the ending block height for backfilling missing traces.
	TracesBackfillEndHeight uint64
	// WalletEnabled sets whether wallet APIs are enabled
	WalletEnabled bool
	// WalletKey used for signing transactions
	WalletKey *ecdsa.PrivateKey
	// MetricsPort defines the port the metric server will listen to
	MetricsPort int
	// IndexOnly configures the gateway to not accept any transactions but only queries of the state
	IndexOnly bool
	// Cache size in units of items in cache, one unit in cache takes approximately 64 bytes
	CacheSize uint
	// ProfilerEnabled sets whether the profiler server is enabled
	ProfilerEnabled bool
	// ProfilerHost is the host for the profiler server will listen to (e.g. localhost, 0.0.0.0)
	ProfilerHost string
	// ProfilerPort is the port for the profiler server
	ProfilerPort int
}
