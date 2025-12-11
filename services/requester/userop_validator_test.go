package requester

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/models"
)

// helper to build *big.Int from int64
func bi(v int64) *big.Int {
	return big.NewInt(v)
}

func TestDecodeValidationData(t *testing.T) {
	// aggregatorOrSigFail = 0 (success)
	vd := DecodeValidationData(big.NewInt(0))
	if vd.SigFailed {
		t.Fatalf("expected sig success")
	}
	if vd.HasAggregator {
		t.Fatalf("expected no aggregator")
	}

	// aggregatorOrSigFail = 1 (failure)
	vd = DecodeValidationData(big.NewInt(1))
	if !vd.SigFailed {
		t.Fatalf("expected sig failure")
	}

	// aggregator address
	addrInt := new(big.Int).SetBytes(common.HexToAddress("0x1234000000000000000000000000000000000000").Bytes())
	vd = DecodeValidationData(addrInt)
	if vd.SigFailed || !vd.HasAggregator {
		t.Fatalf("expected aggregator present, no sig failure")
	}
	if vd.GetAggregatorAddress() != common.HexToAddress("0x1234000000000000000000000000000000000000") {
		t.Fatalf("aggregator address mismatch")
	}
}

func TestPrefundVerification_NoPaymaster(t *testing.T) {
	logger := zerolog.Nop()
	cfg := config.Config{}
	cfg.SetDefaultStakeRequirements()

	validator := &UserOpValidator{
		config: cfg,
		logger: logger,
	}

	userOp := &models.UserOperation{
		Sender:               common.HexToAddress("0x1"),
		PreVerificationGas:   bi(1000),
		CallGasLimit:         bi(2000),
		VerificationGasLimit: bi(3000),
		MaxFeePerGas:         bi(10),
	}

	requiredGas := bi(0).Add(userOp.PreVerificationGas, userOp.CallGasLimit)
	requiredGas.Add(requiredGas, userOp.VerificationGasLimit)
	expectedPrefund := bi(0).Mul(requiredGas, userOp.MaxFeePerGas)

	validationResult := &ValidationResult{
		ReturnInfo: ReturnInfo{
			AccountValidationData:   bi(0),
			PaymasterValidationData: bi(0),
			Prefund: expectedPrefund,
		},
		// minimal stake info to pass stake checks
		SenderInfo:     StakeInfo{Stake: cfg.MinSenderStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		FactoryInfo:    StakeInfo{Stake: cfg.MinFactoryStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		PaymasterInfo:  StakeInfo{Stake: cfg.MinPaymasterStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		AggregatorInfo: AggregatorStakeInfo{StakeInfo: StakeInfo{Stake: cfg.MinAggregatorStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))}},
	}

	if err := validator.validateValidationResult(nil, validationResult, userOp, common.Address{}, 0); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestPrefundVerification_NoPaymaster_Mismatch(t *testing.T) {
	logger := zerolog.Nop()
	cfg := config.Config{}
	cfg.SetDefaultStakeRequirements()

	validator := &UserOpValidator{
		config: cfg,
		logger: logger,
	}

	userOp := &models.UserOperation{
		Sender:               common.HexToAddress("0x1"),
		PreVerificationGas:   bi(1000),
		CallGasLimit:         bi(2000),
		VerificationGasLimit: bi(3000),
		MaxFeePerGas:         bi(10),
	}

	// Expected prefund is larger than actual -> should fail
	expectedPrefund := bi(1) // intentionally wrong

	validationResult := &ValidationResult{
		ReturnInfo: ReturnInfo{
			AccountValidationData:   bi(0),
			PaymasterValidationData: bi(0),
			Prefund: expectedPrefund,
		},
		SenderInfo:     StakeInfo{Stake: cfg.MinSenderStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		FactoryInfo:    StakeInfo{Stake: cfg.MinFactoryStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		PaymasterInfo:  StakeInfo{Stake: cfg.MinPaymasterStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		AggregatorInfo: AggregatorStakeInfo{StakeInfo: StakeInfo{Stake: cfg.MinAggregatorStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))}},
	}

	if err := validator.validateValidationResult(nil, validationResult, userOp, common.Address{}, 0); err == nil {
		t.Fatalf("expected error due to prefund mismatch")
	}
}

func TestPrefundVerification_WithPaymaster(t *testing.T) {
	logger := zerolog.Nop()
	cfg := config.Config{}
	cfg.SetDefaultStakeRequirements()

	validator := &UserOpValidator{
		config: cfg,
		logger: logger,
	}

	userOp := &models.UserOperation{
		Sender:               common.HexToAddress("0x1"),
		PreVerificationGas:   bi(1000),
		CallGasLimit:         bi(2000),
		VerificationGasLimit: bi(3000),
		MaxFeePerGas:         bi(10),
		PaymasterAndData:     []byte{0x01},
	}

	baseRequiredGas := bi(0).Add(userOp.PreVerificationGas, userOp.CallGasLimit)
	baseRequiredGas.Add(baseRequiredGas, userOp.VerificationGasLimit)
	baseExpectedPrefund := bi(0).Mul(baseRequiredGas, userOp.MaxFeePerGas)

	// Simulate paymaster gas added (e.g., +5000 gas * 10)
	actualPrefund := bi(0).Add(baseExpectedPrefund, bi(5000*10))

	validationResult := &ValidationResult{
		ReturnInfo: ReturnInfo{
			AccountValidationData:   bi(0),
			PaymasterValidationData: bi(0),
			Prefund: actualPrefund,
		},
		SenderInfo:     StakeInfo{Stake: cfg.MinSenderStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		FactoryInfo:    StakeInfo{Stake: cfg.MinFactoryStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		PaymasterInfo:  StakeInfo{Stake: cfg.MinPaymasterStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))},
		AggregatorInfo: AggregatorStakeInfo{StakeInfo: StakeInfo{Stake: cfg.MinAggregatorStake, UnstakeDelaySec: bi(int64(cfg.MinUnstakeDelaySec))}},
	}

	if err := validator.validateValidationResult(nil, validationResult, userOp, common.Address{}, 0); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

