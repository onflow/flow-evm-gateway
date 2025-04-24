package replayer

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-go/fvm/evm/emulator"
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/core/tracing"
	"github.com/onflow/go-ethereum/core/types"
	"github.com/onflow/go-ethereum/eth/tracers"
	"github.com/rs/zerolog"
)

const (
	TracerConfig = `{"onlyTopCall":true}`
	TracerName   = "callTracer"
)

func DefaultCallTracer(evmHeight uint64, cfg config.Config) (*tracers.Tracer, error) {
	evmChainConfig := emulator.MakeChainConfig(cfg.EVMNetworkID)
	if !config.IsPrague(evmHeight, cfg.FlowNetworkID) {
		evmChainConfig.PragueTime = nil
	}

	tracer, err := tracers.DefaultDirectory.New(
		TracerName,
		&tracers.Context{},
		json.RawMessage(TracerConfig),
		evmChainConfig,
	)
	if err != nil {
		return nil, err
	}

	return tracer, nil
}

type EVMTracer interface {
	TxTracer(evmHeight uint64) (*tracers.Tracer, error)
	ResetTracer() error
	Collect(txID common.Hash) (json.RawMessage, error)
}

type CallTracerCollector struct {
	tracer        *tracers.Tracer
	resultsByTxID map[common.Hash]json.RawMessage
	config        config.Config
	logger        zerolog.Logger
	lastEvmHeight uint64
}

var _ EVMTracer = (*CallTracerCollector)(nil)

func NewCallTracerCollector(config config.Config, logger zerolog.Logger) *CallTracerCollector {
	return &CallTracerCollector{
		resultsByTxID: make(map[common.Hash]json.RawMessage),
		config:        config,
		logger:        logger.With().Str("component", "evm-tracer").Logger(),
	}
}

func (t *CallTracerCollector) TxTracer(evmHeight uint64) (
	*tracers.Tracer,
	error,
) {
	var err error
	t.tracer, err = DefaultCallTracer(evmHeight, t.config)
	if err != nil {
		return nil, err
	}
	t.lastEvmHeight = evmHeight

	return NewSafeTxTracer(t), nil
}

func (t *CallTracerCollector) ResetTracer() error {
	var err error
	t.tracer, err = DefaultCallTracer(t.lastEvmHeight, t.config)
	return err
}

func (ct *CallTracerCollector) Collect(txID common.Hash) (json.RawMessage, error) {
	// collect the trace result
	result, found := ct.resultsByTxID[txID]
	if !found {
		return nil, fmt.Errorf("trace result for tx:  %s, not found", txID.String())
	}

	// remove the result
	delete(ct.resultsByTxID, txID)

	return result, nil
}

func NewSafeTxTracer(ct *CallTracerCollector) *tracers.Tracer {
	wrapped := &tracers.Tracer{
		Hooks:     &tracing.Hooks{},
		GetResult: ct.tracer.GetResult,
		Stop:      ct.tracer.Stop,
	}

	l := ct.logger

	wrapped.OnTxStart = func(
		vm *tracing.VMContext,
		tx *types.Transaction,
		from common.Address,
	) {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("panic: %v", r)
				}
				l.Err(err).Stack().Msg("OnTxStart trace collection failed")
			}
		}()
		if ct.tracer.OnTxStart != nil {
			ct.tracer.OnTxStart(vm, tx, from)
		}
	}

	wrapped.OnTxEnd = func(receipt *types.Receipt, err error) {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("panic: %v", r)
				}
				l.Err(err).Stack().Msg("OnTxEnd trace collection failed")
			}
		}()
		if ct.tracer.OnTxEnd != nil {
			ct.tracer.OnTxEnd(receipt, err)
		}

		// collect results for the tracer
		res, err := ct.tracer.GetResult()
		if err != nil {
			l.Error().Err(err).Msg("failed to produce trace results")
			return
		}
		ct.resultsByTxID[receipt.TxHash] = res

		// reset tracing to have fresh state
		if err := ct.ResetTracer(); err != nil {
			l.Error().Err(err).Msg("failed to reset tracer")
			return
		}
	}

	wrapped.OnEnter = func(
		depth int,
		typ byte,
		from, to common.Address,
		input []byte,
		gas uint64,
		value *big.Int,
	) {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("panic: %v", r)
				}
				l.Err(err).Stack().Msg("OnEnter trace collection failed")
			}
		}()
		if ct.tracer.OnEnter != nil {
			ct.tracer.OnEnter(depth, typ, from, to, input, gas, value)
		}
	}

	wrapped.OnExit = func(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("panic: %v", r)
				}
				l.Err(err).Stack().Msg("OnExit trace collection failed")
			}
		}()
		if ct.tracer.OnExit != nil {
			ct.tracer.OnExit(depth, output, gasUsed, err, reverted)
		}
	}

	wrapped.OnLog = func(log *types.Log) {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("panic: %v", r)
				}
				l.Err(err).Stack().Msg("OnLog trace collection failed")
			}
		}()
		if ct.tracer.OnLog != nil {
			ct.tracer.OnLog(log)
		}
	}

	return wrapped
}

var NopTracer = &nopTracer{}

var _ EVMTracer = (*nopTracer)(nil)

type nopTracer struct{}

func (n nopTracer) TxTracer(_ uint64) (*tracers.Tracer, error) {
	return nil, nil
}

func (n nopTracer) Collect(_ common.Hash) (json.RawMessage, error) {
	return nil, nil
}

func (n nopTracer) ResetTracer() error {
	return nil
}
