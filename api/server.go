package api

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// httpConfig is the JSON-RPC/HTTP configuration.
type HttpConfig struct {
	Modules            []string
	CorsAllowedOrigins []string
	Vhosts             []string
	prefix             string // path prefix on which to mount http handler
	rpcEndpointConfig
}

// wsConfig is the JSON-RPC/Websocket configuration
type wsConfig struct {
	Origins []string
	Modules []string
	prefix  string // path prefix on which to mount ws handler
	rpcEndpointConfig
}

type rpcEndpointConfig struct {
	jwtSecret              []byte // optional JWT secret
	batchItemLimit         int
	batchResponseSizeLimit int
}

type rpcHandler struct {
	http.Handler
	server *rpc.Server
}

type httpServer struct {
	log      zerolog.Logger
	timeouts rpc.HTTPTimeouts
	mux      http.ServeMux // registered handlers go here

	mu       sync.Mutex
	server   *http.Server
	listener net.Listener // non-nil when server is running

	// HTTP RPC handler things.

	httpConfig  HttpConfig
	httpHandler atomic.Value // *rpcHandler

	// WebSocket handler things.
	wsConfig  wsConfig
	wsHandler atomic.Value // *rpcHandler

	// These are set by setListenAddr.
	endpoint string
	host     string
	port     int

	handlerNames map[string]string
}

const (
	shutdownTimeout = 5 * time.Second
)

func NewHTTPServer(log zerolog.Logger, timeouts rpc.HTTPTimeouts) *httpServer {
	h := &httpServer{log: log, timeouts: timeouts, handlerNames: make(map[string]string)}

	h.httpHandler.Store((*rpcHandler)(nil))
	h.wsHandler.Store((*rpcHandler)(nil))
	return h
}

// setListenAddr configures the listening address of the server.
// The address can only be set while the server isn't running.
func (h *httpServer) SetListenAddr(host string, port int) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.listener != nil && (host != h.host || port != h.port) {
		return fmt.Errorf("HTTP server already running on %s", h.endpoint)
	}

	h.host, h.port = host, port
	h.endpoint = net.JoinHostPort(host, fmt.Sprintf("%d", port))
	return nil
}

// listenAddr returns the listening address of the server.
func (h *httpServer) ListenAddr() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.listener != nil {
		return h.listener.Addr().String()
	}
	return h.endpoint
}

// rpcAllowed returns true when JSON-RPC over HTTP is enabled.
func (h *httpServer) rpcAllowed() bool {
	return h.httpHandler.Load().(*rpcHandler) != nil
}

// enableRPC turns on JSON-RPC over HTTP on the server.
func (h *httpServer) EnableRPC(apis []rpc.API, config HttpConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rpcAllowed() {
		return fmt.Errorf("JSON-RPC over HTTP is already enabled")
	}

	// Create RPC server and handler.
	srv := rpc.NewServer()
	//srv.SetBatchLimits(config.batchItemLimit, config.batchResponseSizeLimit)
	// Register all the APIs exposed by the services
	for _, api := range apis {
		if err := srv.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}
	h.httpConfig = config
	h.httpHandler.Store(&rpcHandler{
		Handler: srv,
		server:  srv,
	})
	return nil
}

// wsAllowed returns true when JSON-RPC over WebSocket is enabled.
func (h *httpServer) wsAllowed() bool {
	return h.wsHandler.Load().(*rpcHandler) != nil
}

// disableWS disables the WebSocket handler. This is internal, the caller must hold h.mu.
func (h *httpServer) disableWS() bool {
	ws := h.wsHandler.Load().(*rpcHandler)
	if ws != nil {
		h.wsHandler.Store((*rpcHandler)(nil))
		ws.server.Stop()
	}
	return ws != nil
}

// start starts the HTTP server if it is enabled and not already running.
func (h *httpServer) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.endpoint == "" || h.listener != nil {
		return nil // already running or not configured
	}

	// Initialize the server.
	h.server = &http.Server{Handler: h}
	if h.timeouts != (rpc.HTTPTimeouts{}) {
		CheckTimeouts(h.log, &h.timeouts)
		h.server.ReadTimeout = h.timeouts.ReadTimeout
		h.server.ReadHeaderTimeout = h.timeouts.ReadHeaderTimeout
		h.server.WriteTimeout = h.timeouts.WriteTimeout
		h.server.IdleTimeout = h.timeouts.IdleTimeout
	}

	// Start the server.
	listener, err := net.Listen("tcp", h.endpoint)
	if err != nil {
		// If the server fails to start, we need to clear out the RPC and WS
		// configuration so they can be configured another time.
		h.disableRPC()
		h.disableWS()
		return err
	}
	h.listener = listener
	go h.server.Serve(listener)

	if h.wsAllowed() {
		url := fmt.Sprintf("ws://%v", listener.Addr())
		if h.wsConfig.prefix != "" {
			url += h.wsConfig.prefix
		}
		log.Info().Msg(fmt.Sprint("WebSocket enabled", "url", url))
	}

	// if server is websocket only, return after logging
	if !h.rpcAllowed() {
		return nil
	}

	// Log http endpoint.
	log.Info().Msg(
		fmt.Sprint(
			"HTTP server started",
			"endpoint",
			listener.Addr(),
			"auth",
			(h.httpConfig.jwtSecret != nil),
			"prefix",
			h.httpConfig.prefix,
			"cors",
			strings.Join(h.httpConfig.CorsAllowedOrigins, ","),
			"vhosts",
			strings.Join(h.httpConfig.Vhosts, ","),
		),
	)

	// Log all handlers mounted on server.
	var paths []string
	for path := range h.handlerNames {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	logged := make(map[string]bool, len(paths))
	for _, path := range paths {
		name := h.handlerNames[path]
		if !logged[name] {
			log.Info().Msg(
				fmt.Sprint(name, " enabled", "url", "http://", listener.Addr().String(), path),
			)
			logged[name] = true
		}
	}
	return nil
}

// disableRPC stops the HTTP RPC handler. This is internal, the caller must hold h.mu.
func (h *httpServer) disableRPC() bool {
	handler := h.httpHandler.Load().(*rpcHandler)
	if handler != nil {
		h.httpHandler.Store((*rpcHandler)(nil))
		handler.server.Stop()
	}
	return handler != nil
}

func (h *httpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// check if ws request and serve if ws enabled
	ws := h.wsHandler.Load().(*rpcHandler)
	if ws != nil && isWebsocket(r) {
		if checkPath(r, h.wsConfig.prefix) {
			ws.ServeHTTP(w, r)
		}
		return
	}

	// if http-rpc is enabled, try to serve request
	rpc := h.httpHandler.Load().(*rpcHandler)
	if rpc != nil {
		// First try to route in the mux.
		// Requests to a path below root are handled by the mux,
		// which has all the handlers registered via Node.RegisterHandler.
		// These are made available when RPC is enabled.
		muxHandler, pattern := h.mux.Handler(r)
		if pattern != "" {
			muxHandler.ServeHTTP(w, r)
			return
		}

		if checkPath(r, h.httpConfig.prefix) {
			rpc.ServeHTTP(w, r)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

// CheckTimeouts ensures that timeout values are meaningful
func CheckTimeouts(logger zerolog.Logger, timeouts *rpc.HTTPTimeouts) {
	if timeouts.ReadTimeout < time.Second {
		log.Warn().Msg(
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
		log.Warn().Msg(
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
		log.Warn().Msg(
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
		log.Warn().Msg(
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

// isWebsocket checks the header of an http request for a websocket upgrade request.
func isWebsocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

const EthNamespace = "eth"

func NewRPCServer() *rpc.Server {
	server := rpc.NewServer()
	server.RegisterName(EthNamespace, &BlockChainAPI{})

	return server
}
