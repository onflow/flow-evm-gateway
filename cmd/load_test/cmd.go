package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var Cmd = &cobra.Command{
	Use:   "load_test",
	Short: "Simulates API traffic on an EVM Gateway node for load testing purposes",
	RunE: func(command *cobra.Command, _ []string) error {
		ctx, cancel := context.WithCancel(command.Context())
		defer cancel()

		g := errgroup.Group{}
		for range workers {
			g.Go(func() error {
				for ctx.Err() == nil {
					err := simulateTraffic()
					if err != nil {
						fmt.Println("Error: ", err)
					}
				}

				return nil
			})
		}

		osSig := make(chan os.Signal, 1)
		signal.Notify(osSig, syscall.SIGINT, syscall.SIGTERM)

		// wait for command to exit or for a shutdown signal
		<-osSig
		log.Info().Msg("OS Signal to shutdown received, shutting down")
		cancel()

		return nil
	},
}

func simulateTraffic() error {
	rpcClient := &RpcClient{url: evmGatewayHost}

	blockNumber, err := rpcClient.EthBlockNumber()
	if err != nil {
		return fmt.Errorf("failed to call `eth_blockNumber`: %w", err)
	}
	fmt.Println("Block Number: ", blockNumber)

	block, err := rpcClient.EthGetBlockByNumber(blockNumber)
	if err != nil {
		return fmt.Errorf("failed to call `eth_getBlockByNumber`: %w", err)
	}
	fmt.Println("Block: ", block)

	blockReceipts, err := rpcClient.EthGetBlockReceipts(blockNumber)
	if err != nil {
		return fmt.Errorf("failed to call `eth_getBlockReceipts`: %w", err)
	}
	fmt.Println("Block Receipts: ", blockReceipts)

	if len(blockReceipts) > 0 {
		receipt := blockReceipts[rand.Intn(len(blockReceipts))]
		if val, ok := receipt["transactionHash"]; ok {
			txHash := val.(string)
			receipt, err := rpcClient.EthGetReceipt(txHash)
			if err != nil {
				return fmt.Errorf("failed to call `eth_getTransactionReceipt`: %w", err)
			}
			fmt.Println("Receipt: ", receipt)

			txTrace, err := rpcClient.DebugTraceTransaction(txHash)
			if err != nil {
				return fmt.Errorf("failed to call `debug_TraceTransaction`: %w", err)
			}
			fmt.Println("Tx Trace: ", txTrace)
		}

		if val, ok := receipt["from"]; ok {
			from := val.(string)
			balance, err := rpcClient.EthGetBalance(from)
			if err != nil {
				return fmt.Errorf("failed to call `eth_getBalance`: %w", err)
			}
			fmt.Println("Balance: ", balance)
		}
	}

	blockTraces, err := rpcClient.DebugTraceBlockByNumber(blockNumber)
	if err != nil {
		return fmt.Errorf("failed to call `debug_traceBlockByNumber`: %w", err)
	}
	fmt.Println("Block Traces: ", blockTraces)

	callResult, err := rpcClient.EthCall()
	if err != nil {
		return fmt.Errorf("failed to call `eth_call`: %w", err)
	}
	fmt.Println("Call Result: ", callResult)

	return nil
}

type RpcResult struct {
	Result json.RawMessage `json:"result"`
	Error  any             `json:"error"`
}

type RpcClient struct {
	url string
}

func (r *RpcClient) request(method string, params string) (json.RawMessage, error) {
	requestURL := fmt.Sprintf(`{"jsonrpc":"2.0","id":0,"method":"%s","params":%s}`, method, params)
	body := bytes.NewReader([]byte(requestURL))
	req, err := http.NewRequest(http.MethodPost, r.url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept-encoding", "identity")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	content, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var resp RpcResult
	err = json.Unmarshal(content, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	return resp.Result, nil
}

func (r *RpcClient) EthBlockNumber() (string, error) {
	rpcRes, err := r.request("eth_blockNumber", "[]")
	if err != nil {
		return "", err
	}

	var blockNumber string
	err = json.Unmarshal(rpcRes, &blockNumber)
	if err != nil {
		return "", err
	}

	return blockNumber, nil
}

func (r *RpcClient) EthGetBlockByNumber(blockNumber string) (string, error) {
	rpcRes, err := r.request("eth_getBlockByNumber", fmt.Sprintf(`["%s",true]`, blockNumber))
	if err != nil {
		return "", err
	}

	return string(rpcRes), nil
}

func (r *RpcClient) EthGetBlockReceipts(blockNumber string) ([]map[string]any, error) {
	rpcRes, err := r.request("eth_getBlockReceipts", fmt.Sprintf(`["%s"]`, blockNumber))
	if err != nil {
		return nil, err
	}

	var blockReceipts []map[string]any
	err = json.Unmarshal(rpcRes, &blockReceipts)
	if err != nil {
		return nil, err
	}

	return blockReceipts, nil
}

func (r *RpcClient) DebugTraceBlockByNumber(blockNumber string) (string, error) {
	tracerConfig := `{"tracer":"callTracer","tracerConfig":{"withLog":true,"onlyTopCall":false}}`
	rpcRes, err := r.request("debug_traceBlockByNumber", fmt.Sprintf(`["%s",%s]`, blockNumber, tracerConfig))
	if err != nil {
		return "", err
	}

	return string(rpcRes), nil
}

func (r *RpcClient) DebugTraceTransaction(txHash string) (string, error) {
	tracerConfig := `{"tracer":"callTracer","tracerConfig":{"withLog":true,"onlyTopCall":false}}`
	rpcRes, err := r.request("debug_traceTransaction", fmt.Sprintf(`["%s",%s]`, txHash, tracerConfig))
	if err != nil {
		return "", err
	}

	return string(rpcRes), nil
}

func (r *RpcClient) EthGetBalance(address string) (*big.Int, error) {
	balanceRes, err := r.request("eth_getBalance", fmt.Sprintf(`["%s", "latest"]`, address))
	if err != nil {
		return nil, err
	}

	var balance hexutil.Big
	err = json.Unmarshal(balanceRes, &balance)
	if err != nil {
		return nil, err
	}

	return balance.ToInt(), nil
}

func (r *RpcClient) EthGetReceipt(hash string) (*types.Receipt, error) {
	rpcRes, err := r.request("eth_getTransactionReceipt", fmt.Sprintf(`["%s"]`, hash))
	if err != nil {
		return nil, err
	}

	var rcp types.Receipt
	err = json.Unmarshal(rpcRes, &rcp)
	if err != nil {
		return nil, err
	}

	return &rcp, nil
}

func (r *RpcClient) EthCall() (string, error) {
	callParams := `{"from":"0x980DbdE8EC2cFebFA46660778afC1cc182EaEADa","to":"0xf19fD4347CafAdd409a9fA090b3AD068272035a1","gas":"0x3CFA39","value":"0x0","input":"0x5148e0ba00000000000000000000000000000000000000000000006c6b935b8bbd400000000000000000000000000000000000000000000000000000000000000000037800000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000000"}`
	rpcRes, err := r.request("eth_call", fmt.Sprintf(`[%s,"latest"]`, callParams))
	if err != nil {
		return "", err
	}

	return string(rpcRes), nil
}

var (
	evmGatewayHost string
	workers        int
)

func init() {
	Cmd.Flags().StringVar(&evmGatewayHost, "evm-gw-host", "http://127.0.0.1:8545", "EVM Gateway host against which to run the load test")
	Cmd.Flags().IntVar(&workers, "workers", 10, "Number of workers to use for concurrent JSON-RPC requests")

	if err := Cmd.MarkFlagRequired("evm-gw-host"); err != nil {
		panic(err)
	}
}
