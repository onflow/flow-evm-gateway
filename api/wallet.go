package api

import (
	"context"
	"fmt"

	evmEmulator "github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/onflow/go-ethereum/rpc"

	errs "github.com/onflow/flow-evm-gateway/api/errors"
	"github.com/onflow/flow-evm-gateway/config"
)

type WalletAPI struct {
	net    *BlockChainAPI
	config *config.Config
}

func NewWalletAPI(config *config.Config, net *BlockChainAPI) *WalletAPI {
	return &WalletAPI{
		net:    net,
		config: config,
	}
}

// Accounts returns the collection of accounts this node manages.
func (w *WalletAPI) Accounts() ([]common.Address, error) {
	return []common.Address{
		crypto.PubkeyToAddress(w.config.WalletKey.PublicKey),
	}, nil
}

// Sign calculates an ECDSA signature for:
// keccak256("\x19Ethereum Signed Message:\n" + len(message) + message).
//
// Note, the produced signature conforms to the secp256k1 curve R, S and V values,
// where the V value will be 27 or 28 for legacy reasons.
//
// The account associated with addr must be unlocked.
//
// https://github.com/ethereum/wiki/wiki/JSON-RPC#eth_sign
func (w *WalletAPI) Sign(
	addr common.Address,
	data hexutil.Bytes,
) (hexutil.Bytes, error) {
	return nil, errs.ErrNotSupported
}

// SignTransaction will sign the given transaction with the from account.
// The node needs to have the private key of the account corresponding with
// the given from address and it needs to be unlocked.
func (w *WalletAPI) SignTransaction(
	ctx context.Context,
	args TransactionArgs,
) (*SignTransactionResult, error) {
	nonce := uint64(0)
	if args.Nonce != nil {
		nonce = uint64(*args.Nonce)
	} else {
		num := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		n, err := w.net.GetTransactionCount(ctx, *args.From, &num)
		if err != nil {
			return nil, err
		}
		nonce = uint64(*n)
	}

	var data []byte
	if args.Data != nil {
		data = *args.Data
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       args.To,
		Value:    args.Value.ToInt(),
		Gas:      uint64(*args.Gas),
		GasPrice: args.GasPrice.ToInt(),
		Data:     data,
	})

	signed, err := types.SignTx(tx, evmEmulator.GetDefaultSigner(), w.config.WalletKey)
	if err != nil {
		return nil, fmt.Errorf("error signing EVM transaction: %w", err)
	}

	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return &SignTransactionResult{
		Raw: raw,
		Tx:  tx,
	}, nil
}

// SendTransaction creates a transaction for the given argument, sign it
// and submit it to the transaction pool.
func (w *WalletAPI) SendTransaction(
	ctx context.Context,
	args TransactionArgs,
) (common.Hash, error) {
	signed, err := w.SignTransaction(ctx, args)
	if err != nil {
		return common.Hash{}, err
	}

	return w.net.SendRawTransaction(ctx, signed.Raw)
}
