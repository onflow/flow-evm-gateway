package api

import (
	"github.com/onflow/go-ethereum/common"
	"github.com/onflow/go-ethereum/common/hexutil"
)

type TxPool struct{}

func NewTxPoolAPI() *TxPool {
	return &TxPool{}
}

type content struct {
	Pending []any
	Queued  []any
}

func (s *TxPool) Content() content {
	return content{}
}

func (s *TxPool) ContentFrom(addr common.Address) content {
	return content{}
}

func (s *TxPool) Status() map[string]hexutil.Uint {
	return map[string]hexutil.Uint{
		"pending": hexutil.Uint(0),
		"queued":  hexutil.Uint(0),
	}
}

func (s *TxPool) Inspect() content {
	return content{}
}
