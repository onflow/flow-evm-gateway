package models

import (
	_ "embed"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/cadence/runtime/common"
	"github.com/onflow/flow-go/fvm/evm/types"
	gethCommon "github.com/onflow/go-ethereum/common"
	gethTypes "github.com/onflow/go-ethereum/core/types"
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

	ev := types.NewTransactionEvent(
		res,
		txEncoded,
		1,
		gethCommon.HexToHash("0x1"),
	)

	location := common.NewAddressLocation(nil, common.Address{0x1}, string(types.EventTypeBlockExecuted))
	cdcEv, err := ev.Payload.ToCadence(location)
	require.NoError(t, err)

	return cdcEv, res
}

func Test_DecodeEVMTransaction(t *testing.T) {
	cdcEv, _ := createTestEvent(t, evmTxBinary)

	decTx, err := decodeTransaction(cdcEv)
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

	decTx, err := decodeTransaction(cdcEv)
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

		tx, err := decodeTransaction(cdcEv)
		require.NoError(t, err)

		encodedTx, err := tx.MarshalBinary()
		require.NoError(t, err)

		decTx, err := UnmarshalTransaction(encodedTx)
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

		tx, err := decodeTransaction(cdcEv)
		require.NoError(t, err)

		encodedTx, err := tx.MarshalBinary()
		require.NoError(t, err)

		decTx, err := UnmarshalTransaction(encodedTx)
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
}

func TestValidateTransaction(t *testing.T) {
	validToAddress := gethCommon.HexToAddress("0x000000000000000000000000000000000000dEaD")
	zeroToAddress := gethCommon.HexToAddress("0x0000000000000000000000000000000000000000")

	smallContractPayload, err := hex.DecodeString("c6888fa1")
	require.NoError(t, err)

	invalidTxData, err := hex.DecodeString("c6888f")
	require.NoError(t, err)

	invalidTxDataLen, err := hex.DecodeString("c6888fa1000000000000000000000000000000000000000000000000000000000000ab")
	require.NoError(t, err)

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
		"small payload for create": {
			tx: gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       nil,
					Value:    big.NewInt(150),
					Gas:      53_000,
					GasPrice: big.NewInt(0),
					Data:     smallContractPayload,
				},
			),
			valid:  false,
			errMsg: "transaction will create a contract, but the payload is suspiciously small (4 bytes)",
		},
		"tx data length < 4": {
			tx: gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       &validToAddress,
					Value:    big.NewInt(150),
					Gas:      153_000,
					GasPrice: big.NewInt(0),
					Data:     invalidTxData,
				},
			),
			valid:  false,
			errMsg: "transaction data is not valid ABI (missing the 4 byte call prefix)",
		},
		"tx data (excluding function selector) not divisible by 32": {
			tx: gethTypes.NewTx(
				&gethTypes.LegacyTx{
					Nonce:    1,
					To:       &validToAddress,
					Value:    big.NewInt(150),
					Gas:      153_000,
					GasPrice: big.NewInt(0),
					Data:     invalidTxDataLen,
				},
			),
			valid:  false,
			errMsg: "transaction data is not valid ABI (length should be a multiple of 32 (was 31))",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateTransaction(tc.tx)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.errMsg)
			}
		})
	}

}
