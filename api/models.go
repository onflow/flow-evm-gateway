package api

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/onflow/flow-evm-gateway/models"
	errs "github.com/onflow/flow-evm-gateway/models/errors"

	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
	"github.com/onflow/go-ethereum/core/types"
)

// TransactionArgs represents the arguments to construct a new transaction
// or a message call.
type TransactionArgs struct {
	From                 *common.Address `json:"from"`
	To                   *common.Address `json:"to"`
	Gas                  *hexutil.Uint64 `json:"gas"`
	GasPrice             *hexutil.Big    `json:"gasPrice"`
	MaxFeePerGas         *hexutil.Big    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *hexutil.Big    `json:"maxPriorityFeePerGas"`
	Value                *hexutil.Big    `json:"value"`
	Nonce                *hexutil.Uint64 `json:"nonce"`

	// We accept "data" and "input" for backwards-compatibility reasons.
	// "input" is the newer name and should be preferred by clients.
	// Issue detail: https://github.com/ethereum/go-ethereum/issues/15628
	Data  *hexutil.Bytes `json:"data"`
	Input *hexutil.Bytes `json:"input"`

	// Introduced by AccessListTxType transaction.
	AccessList *types.AccessList `json:"accessList,omitempty"`
	ChainID    *hexutil.Big      `json:"chainId,omitempty"`
}

func (txArgs TransactionArgs) Validate() error {
	// Prevent accidental erroneous usage of both 'input' and 'data' (show stopper)
	if txArgs.Data != nil && txArgs.Input != nil && !bytes.Equal(*txArgs.Data, *txArgs.Input) {
		return errs.NewInvalidTransactionError(
			errors.New(`ambiguous request: both "data" and "input" are set and are not identical`),
		)
	}

	// Place data on 'data'
	var data []byte
	if txArgs.Input != nil {
		txArgs.Data = txArgs.Input
	}
	if txArgs.Data != nil {
		data = *txArgs.Data
	}

	txDataLen := len(data)

	// Contract creation doesn't validate call data, handle first
	if txArgs.To == nil {
		// Contract creation should contain sufficient data to deploy a contract. A
		// typical error is omitting sender due to some quirk in the javascript call
		// e.g. https://github.com/onflow/go-ethereum/issues/16106.
		if txDataLen == 0 {
			// Prevent sending ether into black hole (show stopper)
			if txArgs.Value.ToInt().Cmp(big.NewInt(0)) > 0 {
				return errs.NewInvalidTransactionError(
					errors.New("transaction will create a contract with value but empty code"),
				)
			}

			// No value submitted at least, critically Warn, but don't blow up
			return errs.NewInvalidTransactionError(errors.New("transaction will create a contract with empty code"))
		}
	}

	// Not a contract creation, validate as a plain transaction
	if txArgs.To != nil {
		to := common.NewMixedcaseAddress(*txArgs.To)

		if bytes.Equal(to.Address().Bytes(), common.Address{}.Bytes()) {
			return errs.NewInvalidTransactionError(errors.New("transaction recipient is the zero address"))
		}
	}

	return nil
}

// AccessListResult returns an optional accesslist
// It's the result of the `debug_createAccessList` RPC call.
// It contains an error if the transaction itself failed.
type AccessListResult struct {
	Accesslist *types.AccessList `json:"accessList"`
	Error      string            `json:"error,omitempty"`
	GasUsed    hexutil.Uint64    `json:"gasUsed"`
}

type FeeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

// AccountResult structs for GetProof
type AccountResult struct {
	Address      common.Address  `json:"address"`
	AccountProof []string        `json:"accountProof"`
	Balance      *hexutil.Big    `json:"balance"`
	CodeHash     common.Hash     `json:"codeHash"`
	Nonce        hexutil.Uint64  `json:"nonce"`
	StorageHash  common.Hash     `json:"storageHash"`
	StorageProof []StorageResult `json:"storageProof"`
}

type StorageResult struct {
	Key   string       `json:"key"`
	Value *hexutil.Big `json:"value"`
	Proof []string     `json:"proof"`
}

// Transaction represents a transaction that will serialize to the RPC representation of a transaction
type Transaction struct {
	BlockHash           *common.Hash             `json:"blockHash"`
	BlockNumber         *hexutil.Big             `json:"blockNumber"`
	From                common.MixedcaseAddress  `json:"from"`
	Gas                 hexutil.Uint64           `json:"gas"`
	GasPrice            *hexutil.Big             `json:"gasPrice"`
	GasFeeCap           *hexutil.Big             `json:"maxFeePerGas,omitempty"`
	GasTipCap           *hexutil.Big             `json:"maxPriorityFeePerGas,omitempty"`
	MaxFeePerBlobGas    *hexutil.Big             `json:"maxFeePerBlobGas,omitempty"`
	Hash                common.Hash              `json:"hash"`
	Input               hexutil.Bytes            `json:"input"`
	Nonce               hexutil.Uint64           `json:"nonce"`
	To                  *common.MixedcaseAddress `json:"to"`
	TransactionIndex    *hexutil.Uint64          `json:"transactionIndex"`
	Value               *hexutil.Big             `json:"value"`
	Type                hexutil.Uint64           `json:"type"`
	Accesses            *types.AccessList        `json:"accessList,omitempty"`
	ChainID             *hexutil.Big             `json:"chainId,omitempty"`
	BlobVersionedHashes []common.Hash            `json:"blobVersionedHashes,omitempty"`
	V                   *hexutil.Big             `json:"v"`
	R                   *hexutil.Big             `json:"r"`
	S                   *hexutil.Big             `json:"s"`
	YParity             *hexutil.Uint64          `json:"yParity,omitempty"`

	size uint64
}

func (tx *Transaction) Size() uint64 {
	return tx.size
}

func NewTransactionResult(
	tx models.Transaction,
	receipt models.StorageReceipt,
	networkID *big.Int,
) (*Transaction, error) {
	res, err := NewTransaction(tx, networkID)
	if err != nil {
		return nil, err
	}

	index := uint64(receipt.TransactionIndex)
	res.TransactionIndex = (*hexutil.Uint64)(&index)
	res.BlockHash = &receipt.BlockHash
	res.BlockNumber = (*hexutil.Big)(receipt.BlockNumber)

	return res, nil
}

func NewTransaction(
	tx models.Transaction,
	networkID *big.Int,
) (*Transaction, error) {
	f, err := tx.From()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to get from value: %w", errs.ErrInternal, err)
	}
	from := common.NewMixedcaseAddress(f)

	var to *common.MixedcaseAddress
	if t := tx.To(); t != nil {
		mixedCaseAddress := common.NewMixedcaseAddress(*t)
		to = &mixedCaseAddress
	}

	v, r, s := tx.RawSignatureValues()

	result := &Transaction{
		Type:     hexutil.Uint64(tx.Type()),
		From:     from,
		Gas:      hexutil.Uint64(tx.Gas()),
		GasPrice: (*hexutil.Big)(tx.GasPrice()),
		Hash:     tx.Hash(),
		Input:    tx.Data(),
		Nonce:    hexutil.Uint64(tx.Nonce()),
		To:       to,
		Value:    (*hexutil.Big)(tx.Value()),
		V:        (*hexutil.Big)(v),
		R:        (*hexutil.Big)(r),
		S:        (*hexutil.Big)(s),
		ChainID:  (*hexutil.Big)(networkID),
		size:     tx.Size(),
	}

	if tx.Type() > types.LegacyTxType {
		al := tx.AccessList()
		yparity := hexutil.Uint64(v.Sign())
		result.Accesses = &al
		result.YParity = &yparity
	}

	if tx.Type() > types.AccessListTxType {
		result.GasFeeCap = (*hexutil.Big)(tx.GasFeeCap())
		result.GasTipCap = (*hexutil.Big)(tx.GasTipCap())
		result.GasPrice = (*hexutil.Big)(tx.GasFeeCap()) // Since BaseFee is `0`, this is the effective gas price the sender is willing to pay.
	}

	if tx.Type() > types.DynamicFeeTxType {
		result.MaxFeePerBlobGas = (*hexutil.Big)(tx.BlobGasFeeCap())
		result.BlobVersionedHashes = tx.BlobHashes()
	}

	return result, nil
}

// SignTransactionResult represents a RLP encoded signed transaction.
type SignTransactionResult struct {
	Raw hexutil.Bytes      `json:"raw"`
	Tx  *types.Transaction `json:"tx"`
}

// OverrideAccount indicates the overriding fields of account during the execution
// of a message call.
// Note, state and stateDiff can't be specified at the same time. If state is
// set, message execution will only use the data in the given state. Otherwise
// if statDiff is set, all diff will be applied first and then execute the call
// message.
type OverrideAccount struct {
	Nonce     *hexutil.Uint64              `json:"nonce"`
	Code      *hexutil.Bytes               `json:"code"`
	Balance   **hexutil.Big                `json:"balance"`
	State     *map[common.Hash]common.Hash `json:"state"`
	StateDiff *map[common.Hash]common.Hash `json:"stateDiff"`
}

// StateOverride is the collection of overridden accounts.
type StateOverride map[common.Address]OverrideAccount

// BlockOverrides is a set of header fields to override.
type BlockOverrides struct {
	Number      *hexutil.Big
	Difficulty  *hexutil.Big
	Time        *hexutil.Uint64
	GasLimit    *hexutil.Uint64
	Coinbase    *common.Address
	Random      *common.Hash
	BaseFee     *hexutil.Big
	BlobBaseFee *hexutil.Big
}

type Block struct {
	Number           hexutil.Uint64   `json:"number"`
	Hash             common.Hash      `json:"hash"`
	ParentHash       common.Hash      `json:"parentHash"`
	Nonce            types.BlockNonce `json:"nonce"`
	Sha3Uncles       common.Hash      `json:"sha3Uncles"`
	LogsBloom        hexutil.Bytes    `json:"logsBloom"`
	TransactionsRoot common.Hash      `json:"transactionsRoot"`
	StateRoot        common.Hash      `json:"stateRoot"`
	ReceiptsRoot     common.Hash      `json:"receiptsRoot"`
	Miner            common.Address   `json:"miner"`
	Difficulty       hexutil.Uint64   `json:"difficulty"`
	TotalDifficulty  hexutil.Uint64   `json:"totalDifficulty"`
	ExtraData        hexutil.Bytes    `json:"extraData"`
	Size             hexutil.Uint64   `json:"size"`
	GasLimit         hexutil.Uint64   `json:"gasLimit"`
	GasUsed          hexutil.Uint64   `json:"gasUsed"`
	Timestamp        hexutil.Uint64   `json:"timestamp"`
	Transactions     interface{}      `json:"transactions"`
	Uncles           []common.Hash    `json:"uncles"`
	MixHash          common.Hash      `json:"mixHash"`
	BaseFeePerGas    hexutil.Big      `json:"baseFeePerGas"`
}

type SyncStatus struct {
	StartingBlock hexutil.Uint64 `json:"startingBlock"`
	CurrentBlock  hexutil.Uint64 `json:"currentBlock"`
	HighestBlock  hexutil.Uint64 `json:"highestBlock"`
}
