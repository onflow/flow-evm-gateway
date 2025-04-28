package replayer

import (
	"encoding/json"
	"fmt"
	"math/big"

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

func DefaultCallTracer(evmChainID *big.Int) (*tracers.Tracer, error) {
	evmChainConfig := emulator.MakeChainConfig(evmChainID)

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
	TxTracer() *tracers.Tracer
	ResetTracer() error
	Collect(txID common.Hash) (json.RawMessage, error)
}

type CallTracerCollector struct {
	tracer        *tracers.Tracer
	resultsByTxID map[common.Hash]json.RawMessage
	logger        zerolog.Logger
	evmChainID    *big.Int
}

var _ EVMTracer = (*CallTracerCollector)(nil)

func NewCallTracerCollector(evmChainID *big.Int, logger zerolog.Logger) (
	*CallTracerCollector,
	error,
) {
	tracer, err := DefaultCallTracer(evmChainID)
	if err != nil {
		return nil, err
	}

	return &CallTracerCollector{
		tracer:        tracer,
		resultsByTxID: make(map[common.Hash]json.RawMessage),
		logger:        logger.With().Str("component", "evm-tracer").Logger(),
		evmChainID:    evmChainID,
	}, nil
}

func (t *CallTracerCollector) TxTracer() *tracers.Tracer {
	return NewSafeTxTracer(t)
}

func (t *CallTracerCollector) ResetTracer() error {
	var err error
	t.tracer, err = DefaultCallTracer(t.evmChainID)
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

func (n nopTracer) TxTracer() *tracers.Tracer {
	return nil
}

func (n nopTracer) Collect(_ common.Hash) (json.RawMessage, error) {
	return nil, nil
}

func (n nopTracer) ResetTracer() error {
	return nil
}
