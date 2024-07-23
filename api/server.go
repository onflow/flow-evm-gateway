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
	"net"
	"net/http"
	"strings"
	"time"

	gethLog "github.com/onflow/go-ethereum/log"
	"github.com/onflow/go-ethereum/rpc"
	"github.com/rs/cors"
	"github.com/rs/zerolog"

	errs "github.com/onflow/flow-evm-gateway/api/errors"
	"github.com/onflow/flow-evm-gateway/config"
	"github.com/onflow/flow-evm-gateway/metrics"
)

type rpcHandler struct {
	http.Handler
	server *rpc.Server
}

type httpServer struct {
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

	config *config.Config

	collector metrics.Collector
}

const (
	shutdownTimeout      = 5 * time.Second
	batchRequestLimit    = 5
	batchResponseMaxSize = 5 * 1000 * 1000 // 5 MB
)

func NewHTTPServer(logger zerolog.Logger, collector metrics.Collector, cfg *config.Config) *httpServer {
	gethLog.Root().SetHandler(gethLog.FuncHandler(func(r *gethLog.Record) error {
		switch r.Lvl {
		case gethLog.LvlInfo:
			logger.Info().Msg(r.Msg)
		case gethLog.LvlError:
			logger.Error().Str("trace", r.Call.String()).Msg(r.Msg)
		default:
			logger.Debug().Msg(r.Msg)
		}

		return nil
	}))

	return &httpServer{
		logger:    logger,
		timeouts:  rpc.DefaultHTTPTimeouts,
		config:    cfg,
		collector: collector,
	}
}

// SetListenAddr configures the listening address of the server.
// The address can only be set while the server is not running.
func (h *httpServer) SetListenAddr(host string, port int) error {
	if h.listener != nil && (host != h.host || port != h.port) {
		return fmt.Errorf("HTTP server already running on %s", h.endpoint)
	}

	h.host, h.port = host, port
	h.endpoint = net.JoinHostPort(host, fmt.Sprintf("%d", port))

	return nil
}

// ListenAddr returns the listening address of the server.
func (h *httpServer) ListenAddr() string {
	if h.listener != nil {
		return h.listener.Addr().String()
	}

	return h.endpoint
}

// EnableRPC turns on JSON-RPC over HTTP on the server.
func (h *httpServer) EnableRPC(apis []rpc.API) error {
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
func (h *httpServer) rpcAllowed() bool {
	return h.httpHandler != nil
}

// EnableWS turns on JSON-RPC over WebSocket on the server.
func (h *httpServer) EnableWS(apis []rpc.API) error {
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
func (h *httpServer) wsAllowed() bool {
	return h.wsHandler != nil
}

// disableWS disables the JSON-RPC over WebSocket handler.
func (h *httpServer) disableWS() bool {
	if h.wsAllowed() {
		h.wsHandler.server.Stop()
		h.wsHandler = nil
		return true
	}

	return false
}

// Start starts the HTTP server if it is enabled and not already running.
func (h *httpServer) Start() error {
	if h.endpoint == "" || h.listener != nil {
		return nil // already running or not configured
	}

	// Initialize the server. Metrics handler is a middleware for gathering metrics
	h.server = &http.Server{Handler: metrics.NewHttpHandler(h, h.collector)}
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
func (h *httpServer) disableRPC() bool {
	if h.rpcAllowed() {
		h.httpHandler.server.Stop()
		h.httpHandler = nil
		return true
	}

	return false
}

func (h *httpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// this overwrites the remote address with the header value, this is used when the server is
	// behind a proxy, and the true source address is overwritten by proxy, but retained in a header.
	if h.config.AddressHeader != "" {
		r.RemoteAddr = r.Header.Get(h.config.AddressHeader)
	}

	// Check if WebSocket request and serve if JSON-RPC over WebSocket is enabled
	if b, err := io.ReadAll(r.Body); err == nil {
		body := make(map[string]any)
		_ = json.Unmarshal(b, &body)

		h.logger.Debug().
			Str("IP", r.RemoteAddr).
			Str("url", r.URL.String()).
			Fields(body).
			Bool("is-ws", isWebSocket(r)).
			Msg("API request")

		r.Body = io.NopCloser(bytes.NewBuffer(b))
		r.Body.Close()
	}

	ws := h.wsHandler
	if ws != nil && isWebSocket(r) {
		ws.ServeHTTP(w, r)
		return
	}

	// enable logging responses
	logW := &loggingResponseWriter{
		ResponseWriter: w,
		logger:         h.logger,
	}

	rpc := recoverHandler(h.logger, h.httpHandler)
	if rpc != nil {
		if checkPath(r, "") {
			rpc.ServeHTTP(logW, r)
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
}

// Stop shuts down the HTTP server.
func (h *httpServer) Stop() {
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

// recoverHandler adds a wrapper to handle panics
func recoverHandler(logger zerolog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			r := recover()
			if r != nil {
				var err error
				switch t := r.(type) {
				case string:
					err = errors.New(t)
				case error:
					err = t
				}

				logger.Error().Err(err).Msg("panic in the http server")
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}

var _ http.ResponseWriter = &loggingResponseWriter{}

type loggingResponseWriter struct {
	http.ResponseWriter
	logger zerolog.Logger
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	body := make(map[string]string)
	_ = json.Unmarshal(data, &body)
	delete(body, "jsonrpc")

	l := w.logger.Debug()

	err := body["error"]
	// only set error level if error is present in response
	if err != "" {
		// don't error log known handled errors
		if !errorIs(err, errs.ErrRateLimit) &&
			!errorIs(err, errs.ErrInvalid) &&
			!errorIs(err, errs.ErrInternal) &&
			!errorIs(err, errs.ErrNotSupported) {
			l = w.logger.Error()
		}
	}

	l.Fields(body).Msg("API response")

	return w.ResponseWriter.Write(data)
}

func (w *loggingResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}

func errorIs(msg string, err error) bool {
	return strings.Contains(msg, err.Error())
}
