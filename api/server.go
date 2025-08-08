// Disclaimer: The implementation & design of `httpServer` is largely inspired
// by https://github.com/ethereum/go-ethereum/blob/master/node/rpcstack.go .
// The types defined on the above file are not exported, so we have extracted
// a minified version of it, for the needs of this EVM Gateway.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	gethLog "github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	slogzerolog "github.com/samber/slog-zerolog"

	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/eth"
	"github.com/onflow/flow-evm-gateway/metrics"
)

type rpcHandler struct {
	http.Handler
	server *rpc.Server
}

type Server struct {
	logger   zerolog.Logger
	timeouts rpc.HTTPTimeouts

	server   *http.Server
	listener net.Listener // non-nil when server is running

	// JSON-RPC over HTTP handler
	httpHandler *rpcHandler

	// JSON-RPC over WebSocket handler
	wsHandler *rpcHandler

	// These are set by SetListenAddr.
	endpoint string
	host     string
	port     int

	config    config.Config
	collector metrics.Collector
}

const (
	shutdownTimeout      = 5 * time.Second
	batchRequestLimit    = 50
	batchResponseMaxSize = 10 * 1000 * 1000 // 10 MB
	contentType          = "application/json"
)

func NewServer(
	logger zerolog.Logger,
	collector metrics.Collector,
	cfg config.Config,
) *Server {
	logger = logger.With().Str("component", "API").Logger()

	zeroSlog := slogzerolog.Option{
		Logger: &logger,
		Level:  slog.LevelError,
	}.NewZerologHandler()
	gethLog.SetDefault(gethLog.NewLogger(zeroSlog))

	return &Server{
		logger:    logger,
		timeouts:  rpc.DefaultHTTPTimeouts,
		config:    cfg,
		collector: collector,
	}
}

// SetListenAddr configures the listening address of the server.
// The address can only be set while the server is not running.
func (h *Server) SetListenAddr(host string, port int) error {
	if h.listener != nil && (host != h.host || port != h.port) {
		return fmt.Errorf("HTTP server already running on: %s", h.endpoint)
	}

	h.host, h.port = host, port
	h.endpoint = net.JoinHostPort(host, fmt.Sprintf("%d", port))

	return nil
}

// ListenAddr returns the listening address of the server.
func (h *Server) ListenAddr() string {
	if h.listener != nil {
		return h.listener.Addr().String()
	}

	return h.endpoint
}

// EnableRPC turns on JSON-RPC over HTTP on the server.
func (h *Server) EnableRPC(apis []rpc.API) error {
	if h.rpcAllowed() {
		return fmt.Errorf("JSON-RPC over HTTP is already enabled")
	}

	// Create RPC server and handler.
	srv := rpc.NewServer()
	srv.SetBatchLimits(batchRequestLimit, batchResponseMaxSize)

	// Register all the APIs exposed by the services
	for _, api := range apis {
		if err := srv.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}

	h.httpHandler = &rpcHandler{
		Handler: corsHandler(srv, []string{"*"}),
		server:  srv,
	}

	return nil
}

// rpcAllowed returns true when JSON-RPC over HTTP is enabled.
func (h *Server) rpcAllowed() bool {
	return h.httpHandler != nil
}

// EnableWS turns on JSON-RPC over WebSocket on the server.
func (h *Server) EnableWS(apis []rpc.API) error {
	if h.wsAllowed() {
		return fmt.Errorf("JSON-RPC over WebSocket is already enabled")
	}

	// Create RPC server and handler.
	srv := rpc.NewServer()

	// Register all the APIs exposed by the services
	for _, api := range apis {
		if err := srv.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}

	h.wsHandler = &rpcHandler{
		Handler: srv.WebsocketHandler([]string{"*"}),
		server:  srv,
	}

	return nil
}

// wsAllowed returns true when JSON-RPC over WebSocket is enabled.
func (h *Server) wsAllowed() bool {
	return h.wsHandler != nil
}

// disableWS disables the JSON-RPC over WebSocket handler.
func (h *Server) disableWS() bool {
	if h.wsAllowed() {
		h.wsHandler.server.Stop()
		h.wsHandler = nil
		return true
	}

	return false
}

// Start starts the HTTP server if it is enabled and not already running.
func (h *Server) Start() error {
	if h.endpoint == "" || h.listener != nil {
		return nil // already running or not configured
	}

	// Initialize the server.
	h.server = &http.Server{Handler: h}
	if h.timeouts != (rpc.HTTPTimeouts{}) {
		CheckTimeouts(h.logger, &h.timeouts)
		h.server.ReadTimeout = h.timeouts.ReadTimeout
		h.server.ReadHeaderTimeout = h.timeouts.ReadHeaderTimeout
		h.server.WriteTimeout = h.timeouts.WriteTimeout
		h.server.IdleTimeout = h.timeouts.IdleTimeout
	}

	// Start the server.
	listener, err := net.Listen("tcp", h.endpoint)
	if err != nil {
		// If the server fails to start, we need to clear out the RPC and WS
		// configurations so they can be configured another time.
		h.disableRPC()
		h.disableWS()
		return err
	}

	h.listener = listener
	go func() {
		err = h.server.Serve(listener)
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				h.logger.Warn().Msg("API server shutdown")
				return
			}
			h.logger.Err(err).Msg("failed to start API server")
			panic(err)
		}
	}()

	if h.rpcAllowed() {
		h.logger.Info().Msgf("JSON-RPC over HTTP enabled: %v", listener.Addr())
	}

	if h.wsAllowed() {
		url := fmt.Sprintf("ws://%v", listener.Addr())
		h.logger.Info().Msgf("JSON-RPC over WebSocket enabled: %s", url)
	}

	return nil
}

// disableRPC stops the JSON-RPC over HTTP handler.
func (h *Server) disableRPC() bool {
	if h.rpcAllowed() {
		h.httpHandler.server.Stop()
		h.httpHandler = nil
		return true
	}

	return false
}

func (h *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// this overwrites the remote address with the header value, this is used when the server is
	// behind a proxy, and the true source address is overwritten by proxy, but retained in a header.
	if h.config.AddressHeader != "" {
		r.RemoteAddr = r.Header.Get(h.config.AddressHeader)
	}

	requestBody := make(map[string]any)
	requestMethod := ""
	if b, err := io.ReadAll(r.Body); err == nil {
		_ = json.Unmarshal(b, &requestBody)

		// Do not log any debug info for methods that are not valid
		// JSON-RPC methods.
		if methodValue, ok := requestBody["method"]; ok {
			if methodStr, ok := methodValue.(string); ok && eth.IsValidMethod(methodStr) {
				requestMethod = methodStr
				h.logger.Debug().
					Str("IP", r.RemoteAddr).
					Str("url", r.URL.String()).
					Fields(requestBody).
					Bool("is-ws", isWebSocket(r)).
					Msg("API request")

			}
		}

		r.Body = io.NopCloser(bytes.NewBuffer(b))
		r.Body.Close()
	}

	// additional response handling
	logW := &responseHandler{
		ResponseWriter: w,
		requestBody:    requestBody,
		log:            h.logger,
		metrics:        h.collector,
	}

	ws := h.wsHandler
	// Check if WebSocket request and serve if JSON-RPC over WebSocket is enabled
	if ws != nil && isWebSocket(r) {
		ws.ServeHTTP(w, r)
		return
	}

	if h.httpHandler != nil && checkPath(r, "") {
		start := time.Now()

		h.httpHandler.ServeHTTP(logW, r)

		if eth.IsValidMethod(requestMethod) {
			h.collector.MeasureRequestDuration(start, requestMethod)
		}

		return
	}

	w.WriteHeader(http.StatusNotFound)
}

// Stop shuts down the HTTP server.
func (h *Server) Stop() {
	if h.listener == nil {
		return // not running
	}

	// Shut down the server.
	httpHandler := h.httpHandler
	if httpHandler != nil {
		httpHandler.server.Stop()
		h.httpHandler = nil
	}

	wsHandler := h.wsHandler
	if wsHandler != nil {
		wsHandler.server.Stop()
		h.wsHandler = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := h.server.Shutdown(ctx)
	if err != nil && err == ctx.Err() {
		h.logger.Warn().Msg("HTTP server graceful shutdown timed out")
		h.server.Close()
	}

	h.listener.Close()
	h.logger.Info().Msgf(
		"HTTP server stopped, endpoint: %s", h.listener.Addr(),
	)

	// Clear out everything to allow re-configuring it later.
	h.host, h.port, h.endpoint = "", 0, ""
	h.server, h.listener = nil, nil
}

// CheckTimeouts ensures that timeout values are meaningful
func CheckTimeouts(logger zerolog.Logger, timeouts *rpc.HTTPTimeouts) {
	if timeouts.ReadTimeout < time.Second {
		logger.Warn().Msg(
			fmt.Sprint(
				"Sanitizing invalid HTTP read timeout",
				"provided",
				timeouts.ReadTimeout,
				"updated",
				rpc.DefaultHTTPTimeouts.ReadTimeout,
			),
		)
		timeouts.ReadTimeout = rpc.DefaultHTTPTimeouts.ReadTimeout
	}
	if timeouts.ReadHeaderTimeout < time.Second {
		logger.Warn().Msg(
			fmt.Sprint(
				"Sanitizing invalid HTTP read header timeout",
				"provided",
				timeouts.ReadHeaderTimeout,
				"updated",
				rpc.DefaultHTTPTimeouts.ReadHeaderTimeout,
			),
		)
		timeouts.ReadHeaderTimeout = rpc.DefaultHTTPTimeouts.ReadHeaderTimeout
	}
	if timeouts.WriteTimeout < time.Second {
		logger.Warn().Msg(
			fmt.Sprint(
				"Sanitizing invalid HTTP write timeout",
				"provided",
				timeouts.WriteTimeout,
				"updated",
				rpc.DefaultHTTPTimeouts.WriteTimeout,
			),
		)
		timeouts.WriteTimeout = rpc.DefaultHTTPTimeouts.WriteTimeout
	}
	if timeouts.IdleTimeout < time.Second {
		logger.Warn().Msg(
			fmt.Sprint(
				"Sanitizing invalid HTTP idle timeout",
				"provided",
				timeouts.IdleTimeout,
				"updated",
				rpc.DefaultHTTPTimeouts.IdleTimeout,
			),
		)
		timeouts.IdleTimeout = rpc.DefaultHTTPTimeouts.IdleTimeout
	}
}

// checkPath checks whether a given request URL matches a given path prefix.
func checkPath(r *http.Request, path string) bool {
	// if no prefix has been specified, request URL must be on root
	if path == "" {
		return r.URL.Path == "/"
	}

	// otherwise, check to make sure prefix matches
	return len(r.URL.Path) >= len(path) && r.URL.Path[:len(path)] == path
}

// isWebSocket checks the header of an HTTP request for a WebSocket upgrade request.
func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func corsHandler(srv http.Handler, allowedOrigins []string) http.Handler {
	// disable CORS support if user has not specified a custom CORS configuration
	if len(allowedOrigins) == 0 {
		return srv
	}
	c := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{http.MethodPost, http.MethodGet},
		AllowedHeaders: []string{"*"},
		MaxAge:         600,
	})
	return c.Handler(srv)
}

var _ http.ResponseWriter = &responseHandler{}

// responseHandler handles server responses.
// Since we reuse go-ethereum server implementation we don't have access to handler logic,
// so we rely on parsing responses and triggering actions to add our logic.
// todo we should replace go-ethereum server implementation with our own so we have more control
type responseHandler struct {
	http.ResponseWriter
	requestBody map[string]any
	log         zerolog.Logger
	metrics     metrics.Collector
}

const errCodePanic = -32603

type jsonError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type jsonMessage struct {
	Version string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Error   *jsonError      `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func (w *responseHandler) Write(data []byte) (int, error) {
	w.ResponseWriter.Header().Set("content-type", contentType)

	var message *jsonMessage
	// if we couldn't parse response just return it as fallback
	if err := json.Unmarshal(data, &message); err != nil {
		return w.ResponseWriter.Write(data)
	}

	// create a default debug logger
	l := w.log.With().Fields(w.requestBody).Logger()
	log := l.Debug()

	// handle possible panics inside endpoints
	if message.Error != nil && message.Error.Code == errCodePanic {
		w.metrics.ServerPanicked(message.Error.Message)
	}

	// It's an error response and requires special treatment.
	if message.Error != nil {
		// The logic below is taken from:
		// https://github.com/ethereum/go-ethereum/blob/v1.15.10/rpc/http.go#L265-L288
		//
		// In case of a timeout error, the response must be written before the HTTP
		// server's write timeout occurs. So we need to flush the response. The
		// Content-Length header also needs to be set to ensure the client knows
		// when it has the full response.
		encdata, err := json.Marshal(message)
		if err != nil {
			return 0, err
		}
		w.ResponseWriter.Header().Set("content-length", strconv.Itoa(len(encdata)))

		// If this request is wrapped in a handler that might remove Content-Length (such
		// as the automatic gzip we do in package node), we need to ensure the HTTP server
		// doesn't perform chunked encoding. In case WriteTimeout is reached, the chunked
		// encoding might not be finished correctly, and some clients do not like it when
		// the final chunk is missing.
		w.ResponseWriter.Header().Set("transfer-encoding", "identity")

		_, err = w.ResponseWriter.Write(encdata)
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
		return 0, err
	}

	// log all response results of successful requests,
	// as errors are logged with error log level.
	r, _ := message.Result.MarshalJSON()
	log.RawJSON("result", r).Msg("API response")

	return w.ResponseWriter.Write(data)
}

func (w *responseHandler) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseHandler) Header() http.Header {
	return w.ResponseWriter.Header()
}
