package api

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type TxPool struct{}

func NewTxPoolAPI() *TxPool {
	return &TxPool{}
}

type content struct {
	Pending any `json:"pending"`
	Queued  any `json:"queued"`
}

func emptyPool() content {
	return content{
		Pending: struct{}{},
		Queued:  struct{}{},
	}
}

func (s *TxPool) Content() content {
	return emptyPool()
}

func (s *TxPool) ContentFrom(addr common.Address) content {
	return emptyPool()
}

func (s *TxPool) Status() map[string]hexutil.Uint {
	return map[string]hexutil.Uint{
		"pending": hexutil.Uint(0),
		"queued":  hexutil.Uint(0),
	}
}

func (s *TxPool) Inspect() content {
	return emptyPool()
}
