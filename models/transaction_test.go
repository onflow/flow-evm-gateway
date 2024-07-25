package models

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/flow-go/fvm/evm/events"
	"github.com/onflow/flow-go/fvm/evm/types"
	flowGo "github.com/onflow/flow-go/model/flow"
	gethCommon "github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core"
	"github.com/onflow/go-ethereum/core/txpool"
	gethTypes "github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/crypto"
	"github.com/onflow/go-ethereum/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed fixtures/transaction_call.bin
var evmTxBinary string

//go:embed fixtures/direct_call.bin
var directCallBinary string

func createTestEvent(t *testing.T, txBinary string) (cadence.Event, *types.Result) {
	txEncoded, err := hex.DecodeString(txBinary)
	require.NoError(t, err)

	var txHash gethCommon.Hash
	var txType uint8
	if txEncoded[0] == types.DirectCallTxType {
		directCall, err := types.DirectCallFromEncoded(txEncoded)
		require.NoError(t, err)

		txHash = directCall.Hash()

		txType = types.DirectCallTxType
	} else {
		gethTx := &gethTypes.Transaction{}
		err := gethTx.UnmarshalBinary(txEncoded)
		require.NoError(t, err)

		txHash = gethTx.Hash()
		txType = gethTx.Type()
	}

	res := &types.Result{
		VMError:                 nil,
		TxType:                  txType,
		GasConsumed:             1337,
		DeployedContractAddress: &types.Address{0x5, 0x6, 0x7},
		ReturnedData:            []byte{0x55},
		Logs: []*gethTypes.Log{{
			Address: gethCommon.Address{0x1, 0x2},
			Topics:  []gethCommon.Hash{{0x5, 0x6}, {0x7, 0x8}},
		}, {
			Address: gethCommon.Address{0x3, 0x5},
			Topics:  []gethCommon.Hash{{0x2, 0x66}, {0x7, 0x1}},
		}},
		TxHash: txHash,
	}

	ev := events.NewTransactionEvent(
		res,
		txEncoded,
		1,
	)

	cdcEv, err := ev.Payload.ToCadence(flowGo.Previewnet)
	require.NoError(t, err)

	return cdcEv, res
}

func Test_DecodeEVMTransaction(t *testing.T) {
	cdcEv, _ := createTestEvent(t, evmTxBinary)

	decTx, _, err := decodeTransactionEvent(cdcEv)
	require.NoError(t, err)
	require.IsType(t, TransactionCall{}, decTx)

	txHash := decTx.Hash()

	v, r, s := decTx.RawSignatureValues()

	from, err := decTx.From()
	require.NoError(t, err)

	assert.Equal(
		t,
		gethCommon.HexToHash("0xe414f90fea2aebd75e8c0b3b6a4a0c9928e86c16ea724343d884f40bfe2c4c6b"),
		txHash,
	)
	assert.Equal(t, big.NewInt(1327), v)
	assert.Equal(
		t,
		"70570792731140625669797097963380383355394381124817754245181559778290488838246",
		r.String(),
	)
	assert.Equal(
		t,
		"39763645764758347623445260367025516531172546351546206339075417954057005180499",
		s.String(),
	)
	assert.Equal(
		t,
		gethCommon.HexToAddress("0x658Bdf435d810C91414eC09147DAA6DB62406379"),
		from,
	)
	assert.Nil(t, decTx.To())
	assert.Len(t, decTx.Data(), 264)
	assert.Equal(t, uint64(0), decTx.Nonce())
	assert.Equal(t, big.NewInt(0), decTx.Value())
	assert.Equal(t, uint8(0), decTx.Type())
	assert.Equal(t, uint64(125_000), decTx.Gas())
	assert.Equal(t, big.NewInt(0), decTx.GasPrice())
	assert.Equal(t, uint64(0), decTx.BlobGas())
	assert.Equal(t, uint64(347), decTx.Size())
}

func Test_DecodeDirectCall(t *testing.T) {
	cdcEv, _ := createTestEvent(t, directCallBinary)

	decTx, _, err := decodeTransactionEvent(cdcEv)
	require.NoError(t, err)
	require.IsType(t, DirectCall{}, decTx)

	txHash := decTx.Hash()
	require.NoError(t, err)

	v, r, s := decTx.RawSignatureValues()

	from, err := decTx.From()
	require.NoError(t, err)

	assert.Equal(
		t,
		gethCommon.HexToHash("0xb055748f36d6bbe99a7ab5e45202b5c095ceda985dec0cc2a8747fd88c80c8c9"),
		txHash,
	)
	assert.Equal(t, big.NewInt(255), v)
	assert.Equal(t, new(big.Int).SetBytes(from.Bytes()), r)
	assert.Equal(t, big.NewInt(1), s)
	assert.Equal(
		t,
		gethCommon.HexToAddress("0x0000000000000000000000010000000000000000"),
		from,
	)
	assert.Equal(
		t,
		gethCommon.HexToAddress("0x000000000000000000000002ef6737ccBbAa9977"),
		*decTx.To(),
	)
	assert.Empty(t, decTx.Data())
	assert.Equal(t, uint64(0), decTx.Nonce())
	assert.Equal(t, big.NewInt(10000000000), decTx.Value())
	assert.Equal(t, uint8(gethTypes.LegacyTxType), decTx.Type())
	assert.Equal(t, uint64(23_300), decTx.Gas())
	assert.Equal(t, big.NewInt(0), decTx.GasPrice())
	assert.Equal(t, uint64(0), decTx.BlobGas())
	assert.Equal(t, uint64(59), decTx.Size())
}

func Test_UnmarshalTransaction(t *testing.T) {
	t.Parallel()

	t.Run("with TransactionCall value", func(t *testing.T) {
		t.Parallel()

		cdcEv, _ := createTestEvent(t, evmTxBinary)

		tx, _, err := decodeTransactionEvent(cdcEv)
		require.NoError(t, err)

		encodedTx, err := tx.MarshalBinary()
		require.NoError(t, err)

		decTx, err := UnmarshalTransaction(encodedTx, DirectCallHashCalculationBlockHeightChange)
		require.NoError(t, err)
		require.IsType(t, TransactionCall{}, decTx)

		txHash := decTx.Hash()

		v, r, s := decTx.RawSignatureValues()

		from, err := decTx.From()
		require.NoError(t, err)

		assert.Equal(
			t,
			gethCommon.HexToHash("0xe414f90fea2aebd75e8c0b3b6a4a0c9928e86c16ea724343d884f40bfe2c4c6b"),
			txHash,
		)
		assert.Equal(t, big.NewInt(1327), v)
		assert.Equal(
			t,
			"70570792731140625669797097963380383355394381124817754245181559778290488838246",
			r.String(),
		)
		assert.Equal(
			t,
			"39763645764758347623445260367025516531172546351546206339075417954057005180499",
			s.String(),
		)
		assert.Equal(
			t,
			gethCommon.HexToAddress("0x658Bdf435d810C91414eC09147DAA6DB62406379"),
			from,
		)
		assert.Nil(t, decTx.To())
		assert.Len(t, decTx.Data(), 264)
		assert.Equal(t, uint64(0), decTx.Nonce())
		assert.Equal(t, big.NewInt(0), decTx.Value())
		assert.Equal(t, uint8(0), decTx.Type())
		assert.Equal(t, uint64(125_000), decTx.Gas())
		assert.Equal(t, big.NewInt(0), decTx.GasPrice())
		assert.Equal(t, uint64(0), decTx.BlobGas())
		assert.Equal(t, uint64(347), decTx.Size())
	})

	t.Run("with DirectCall value", func(t *testing.T) {
		t.Parallel()

		cdcEv, _ := createTestEvent(t, directCallBinary)

		tx, _, err := decodeTransactionEvent(cdcEv)
		require.NoError(t, err)

		encodedTx, err := tx.MarshalBinary()
		require.NoError(t, err)

		decTx, err := UnmarshalTransaction(encodedTx, DirectCallHashCalculationBlockHeightChange)
		require.NoError(t, err)
		require.IsType(t, DirectCall{}, decTx)

		txHash := decTx.Hash()

		v, r, s := decTx.RawSignatureValues()

		from, err := decTx.From()
		require.NoError(t, err)

		assert.Equal(
			t,
			gethCommon.HexToHash("0xb055748f36d6bbe99a7ab5e45202b5c095ceda985dec0cc2a8747fd88c80c8c9"),
			txHash,
		)
		assert.Equal(t, big.NewInt(255), v)
		assert.Equal(t, new(big.Int).SetBytes(from.Bytes()), r)
		assert.Equal(t, big.NewInt(1), s)
		assert.Equal(
			t,
			gethCommon.HexToAddress("0x0000000000000000000000010000000000000000"),
			from,
		)
		assert.Equal(
			t,
			gethCommon.HexToAddress("0x000000000000000000000002ef6737ccBbAa9977"),
			*decTx.To(),
		)
		assert.Empty(t, decTx.Data())
		assert.Equal(t, uint64(0), decTx.Nonce())
		assert.Equal(t, big.NewInt(10000000000), decTx.Value())
		assert.Equal(t, uint8(gethTypes.LegacyTxType), decTx.Type())
		assert.Equal(t, uint64(23_300), decTx.Gas())
		assert.Equal(t, big.NewInt(0), decTx.GasPrice())
		assert.Equal(t, uint64(0), decTx.BlobGas())
		assert.Equal(t, uint64(59), decTx.Size())
	})

	t.Run("with DirectCall hash calculation change", func(t *testing.T) {
		t.Parallel()

		DirectCallHashCalculationBlockHeightChange = 10

		cdcEv, _ := createTestEvent(t, directCallBinary)

		tx, _, err := decodeTransactionEvent(cdcEv)
		require.NoError(t, err)

		encodedTx, err := tx.MarshalBinary()
		require.NoError(t, err)

		// blockHeight is greater than DirectCallHashCalculationBlockHeightChange
		// which means we use the new hash calculation
		decTx, err := UnmarshalTransaction(encodedTx, DirectCallHashCalculationBlockHeightChange+2)
		require.NoError(t, err)
		require.IsType(t, DirectCall{}, decTx)

		v, r, s := decTx.RawSignatureValues()

		from, err := decTx.From()
		require.NoError(t, err)

		newHash := decTx.Hash()

		assert.Equal(
			t,
			gethCommon.HexToHash("0xb055748f36d6bbe99a7ab5e45202b5c095ceda985dec0cc2a8747fd88c80c8c9"),
			newHash,
		)
		assert.Equal(t, big.NewInt(255), v)
		assert.Equal(t, new(big.Int).SetBytes(from.Bytes()), r)
		assert.Equal(t, big.NewInt(1), s)

		// blockHeight is less than DirectCallHashCalculationBlockHeightChange
		// which means we use the old hash calculation
		decTx, err = UnmarshalTransaction(encodedTx, DirectCallHashCalculationBlockHeightChange-2)
		require.NoError(t, err)
		require.IsType(t, DirectCall{}, decTx)

		v, r, s = decTx.RawSignatureValues()

		from, err = decTx.From()
		require.NoError(t, err)

		oldHash := decTx.Hash()

		assert.Equal(
			t,
			gethCommon.HexToHash("0xe090f3a66f269d436e4185551d790d923f53a2caabf475c18d60bf1f091813d9"),
			oldHash,
		)
		assert.Equal(t, big.NewInt(255), v)
		assert.Equal(t, new(big.Int).SetBytes(from.Bytes()), r)
		assert.Equal(t, big.NewInt(1), s)

		assert.NotEqual(t, newHash, oldHash)
	})
}

func TestValidateTransaction(t *testing.T) {
	validToAddress := gethCommon.HexToAddress("0x000000000000000000000000000000000000dEaD")
	zeroToAddress := gethCommon.HexToAddress("0x0000000000000000000000000000000000000000")

	tests := map[string]struct {
		tx     *gethTypes.Transaction
		valid  bool
		errMsg string
	}{
		"valid transaction": {
			tx: gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       &validToAddress,
					Value:    big.NewInt(0),
					Gas:      25_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  true,
			errMsg: "",
		},
		"send to 0 address": {
			tx: gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       &zeroToAddress,
					Value:    big.NewInt(0),
					Gas:      25_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  false,
			errMsg: "transaction recipient is the zero address",
		},
		"create empty contract (no value)": {
			tx: gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(0),
					Gas:      53_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  false,
			errMsg: "transaction will create a contract with empty code",
		},
		"create empty contract (with value)": {
			tx: gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(150),
					Gas:      53_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			valid:  false,
			errMsg: "transaction will create a contract with value but empty code",
		},
	}

	head := &gethTypes.Header{
		Number:   big.NewInt(20_182_324),
		Time:     uint64(time.Now().Unix()),
		GasLimit: 30_000_000,
	}
	emulatorConfig := emulator.NewConfig(
		emulator.WithChainID(types.FlowEVMPreviewNetChainID),
		emulator.WithBlockNumber(head.Number),
		emulator.WithBlockTime(head.Time),
	)
	signer := emulator.GetSigner(emulatorConfig)
	opts := &txpool.ValidationOptions{
		Config: emulatorConfig.ChainConfig,
		Accept: 0 |
			1<<gethTypes.LegacyTxType |
			1<<gethTypes.AccessListTxType |
			1<<gethTypes.DynamicFeeTxType |
			1<<gethTypes.BlobTxType,
		MaxSize: TxMaxSize,
		MinTip:  new(big.Int),
	}

	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			tx, err := gethTypes.SignTx(
				tc.tx,
				signer,
				key,
			)
			require.NoError(t, err)

			err = ValidateTransaction(tx, head, signer, opts)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.errMsg)
			}
		})
	}

}

func TestValidateConsensusRules(t *testing.T) {

	head := &gethTypes.Header{
		Number:   big.NewInt(20_182_324),
		Time:     uint64(time.Now().Unix()),
		GasLimit: 30_000_000,
	}
	emulatorConfig := emulator.NewConfig(
		emulator.WithChainID(types.FlowEVMPreviewNetChainID),
		emulator.WithBlockNumber(head.Number),
		emulator.WithBlockTime(head.Time),
	)
	signer := emulator.GetSigner(emulatorConfig)
	chainConfig := emulatorConfig.ChainConfig
	opts := &txpool.ValidationOptions{
		Config: chainConfig,
		Accept: 0 |
			1<<gethTypes.LegacyTxType |
			1<<gethTypes.AccessListTxType |
			1<<gethTypes.DynamicFeeTxType |
			1<<gethTypes.BlobTxType,
		MaxSize: TxMaxSize,
		MinTip:  new(big.Int),
	}

	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	t.Run("not supported tx type", func(t *testing.T) {
		// We do not accept `gethTypes.LegacyTx` in the options below
		opts := &txpool.ValidationOptions{
			Config: chainConfig,
			Accept: 0 |
				1<<gethTypes.AccessListTxType |
				1<<gethTypes.DynamicFeeTxType,
			MaxSize: TxMaxSize,
			MinTip:  new(big.Int),
		}

		tx := makeSignedTx(53_000, 0, key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction type not supported: tx type 0 not supported by this pool",
		)
	})

	t.Run("tx size limits", func(t *testing.T) {
		// Compute maximal data size for transactions (lower bound).
		//
		// It is assumed the fields in the transaction (except of the data) are:
		//   - nonce     <= 32 bytes
		//   - gasTip    <= 32 bytes
		//   - gasLimit  <= 32 bytes
		//   - recipient == 20 bytes
		//   - value     <= 32 bytes
		//   - signature == 65 bytes
		// All those fields are summed up to at most 213 bytes.
		baseSize := uint64(213)
		dataSize := TxMaxSize - baseSize
		gasLimit := uint64(2_500_000)

		// Try adding a transaction with maximal allowed size
		tx := makeSignedTx(gasLimit, dataSize, key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)
		require.NoError(t, err)

		// Try adding a transaction with random allowed size
		tx = makeSignedTx(gasLimit, uint64(rand.Intn(int(dataSize))), key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)
		require.NoError(t, err)

		// Try adding a transaction of minimal not allowed size
		tx = makeSignedTx(gasLimit, TxMaxSize, key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			fmt.Sprintf("oversized data: transaction size %d, limit 131072", tx.Size()),
		)

		// Try adding a transaction of random not allowed size
		txSize := dataSize + 1 + uint64(rand.Intn(10*TxMaxSize))
		tx = makeSignedTx(gasLimit, txSize, key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			fmt.Sprintf("oversized data: transaction size %d, limit 131072", tx.Size()),
		)
	})

	t.Run("check only fork-enabled transactions are accepted", func(t *testing.T) {
		// We are not yet in Berlin fork
		chainConfig.BerlinBlock = big.NewInt(19_182_324)
		head.Number = big.NewInt(18_182_324)

		tx := dynamicFeeTx(100, big.NewInt(1), big.NewInt(2), key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction type not supported: type 2 rejected, pool not yet in Berlin",
		)

		// We are in Berlin fork, but not in London
		chainConfig.BerlinBlock = big.NewInt(17_182_324)
		chainConfig.LondonBlock = big.NewInt(19_182_524)
		head.Number = big.NewInt(18_182_324)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"transaction type not supported: type 2 rejected, pool not yet in London",
		)

		// TODO(m-Peter): Add assertions for BlobTxType

		// Cleanup
		chainConfig.BerlinBlock = big.NewInt(0)
		chainConfig.LondonBlock = big.NewInt(0)
		head.Number = big.NewInt(20_182_324)
	})

	t.Run("init code size exceeded", func(t *testing.T) {
		dataLen := params.MaxInitCodeSize + 5_000
		data := make([]byte, dataLen)
		n, err := crand.Read(data)
		require.Equal(t, n, dataLen)
		require.NoError(t, err)

		tx, err := gethTypes.SignTx(
			gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(0),
					Gas:      7_500_000,
					GasPrice: big.NewInt(0),
					Data:     data,
				},
			),
			signer,
			key,
		)
		require.NoError(t, err)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"max initcode size exceeded: code size 54152, limit 49152",
		)
	})

	t.Run("negative value", func(t *testing.T) {
		tx, err := gethTypes.SignTx(
			gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(-1500),
					Gas:      7_500_000,
					GasPrice: big.NewInt(0),
					Data:     []byte{},
				},
			),
			signer,
			key,
		)
		require.NoError(t, err)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			txpool.ErrNegativeValue.Error(),
		)
	})

	t.Run("tx gas limit higher than block gas limit", func(t *testing.T) {
		head.GasLimit = 5_000_000
		tx := makeSignedTx(7_500_000, 5_000, key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"exceeds block gas limit",
		)

		// Cleanup
		head.GasLimit = 30_000_000
	})

	t.Run("very high fee values", func(t *testing.T) {
		veryBigNumber := big.NewInt(1)
		veryBigNumber.Lsh(veryBigNumber, 300)

		tx := dynamicFeeTx(100, big.NewInt(1), veryBigNumber, key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			core.ErrTipVeryHigh.Error(),
		)

		tx2 := dynamicFeeTx(100, veryBigNumber, big.NewInt(1), key, signer)

		err = txpool.ValidateTransaction(tx2, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			core.ErrFeeCapVeryHigh.Error(),
		)
	})

	t.Run("gas tip above gas fee cap", func(t *testing.T) {
		tx := dynamicFeeTx(100, big.NewInt(1), big.NewInt(2), key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			core.ErrTipAboveFeeCap.Error(),
		)
	})

	t.Run("invalid sender", func(t *testing.T) {
		tx := makeSignedTx(55_000, 5_000, key, signer)

		err = txpool.ValidateTransaction(tx, head, gethTypes.FrontierSigner{}, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			"invalid sender",
		)
	})

	t.Run("intrinsic gas too low", func(t *testing.T) {
		gasLimit := uint64(100)
		tx := makeSignedTx(gasLimit, 0, key, signer)

		err = txpool.ValidateTransaction(tx, head, signer, opts)

		require.Error(t, err)
		assert.ErrorContains(
			t,
			err,
			fmt.Sprintf("%s: gas %v, minimum needed %v", core.ErrIntrinsicGas.Error(), gasLimit, 21_000),
		)
	})
}

func makeSignedTx(
	gasLimit uint64,
	dataBytes uint64,
	key *ecdsa.PrivateKey,
	signer gethTypes.Signer,
) *gethTypes.Transaction {
	data := make([]byte, dataBytes)
	_, err := crand.Read(data)
	if err != nil {
		panic(err)
	}

	tx, err := gethTypes.SignTx(
		gethTypes.NewTransaction(
			0,
			gethCommon.Address{},
			big.NewInt(100),
			gasLimit,
			big.NewInt(150),
			data,
		),
		signer,
		key,
	)
	if err != nil {
		panic(err)
	}

	return tx
}

func dynamicFeeTx(
	gasLimit uint64,
	gasFeeCap *big.Int,
	gasTipCap *big.Int,
	key *ecdsa.PrivateKey,
	signer gethTypes.Signer,
) *gethTypes.Transaction {
	tx, err := gethTypes.SignNewTx(
		key,
		signer,
		&gethTypes.DynamicFeeTx{
			ChainID:    emulator.DefaultChainConfig.ChainID,
			Nonce:      0,
			GasTipCap:  gasTipCap,
			GasFeeCap:  gasFeeCap,
			Gas:        gasLimit,
			To:         nil,
			Value:      big.NewInt(100),
			Data:       []byte{},
			AccessList: gethTypes.AccessList{},
		},
	)
	if err != nil {
		panic(err)
	}

	return tx
}
