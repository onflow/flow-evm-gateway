package api

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// HttpConfig is the JSON-RPC/HTTP configuration.
type HttpConfig struct {
	Modules            []string
	CorsAllowedOrigins []string
	Vhosts             []string
	prefix             string // path prefix on which to mount http handler
	jwtSecret          []byte // optional JWT secret
}

// WSConfig is the JSON-RPC/WebSocket configuration
type WSConfig struct {
	Origins   []string
	Modules   []string
	prefix    string // path prefix on which to mount ws handler
	jwtSecret []byte // optional JWT secret
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

	// JSON-RPC/HTTP handler things.
	httpConfig  HttpConfig
	httpHandler atomic.Value // *rpcHandler

	// JSON-RPC/WebSocket handler things.
	wsConfig  WSConfig
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
	// TODO: This is only added in go-ethereum@v1.13.2
	// srv.SetBatchLimits(config.batchItemLimit, config.batchResponseSizeLimit)
	// Register all the APIs exposed by the services
	for _, api := range apis {
		if err := srv.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}
	h.httpConfig = config
	h.httpHandler.Store(&rpcHandler{
		Handler: NewHTTPHandlerStack(srv, config.CorsAllowedOrigins, config.Vhosts, config.jwtSecret),
		server:  srv,
	})
	return nil
}

// NewHTTPHandlerStack returns wrapped http-related handlers
func NewHTTPHandlerStack(srv http.Handler, cors []string, vhosts []string, jwtSecret []byte) http.Handler {
	// Wrap the CORS-handler within a host-handler
	handler := newCorsHandler(srv, cors)
	handler = newVHostHandler(vhosts, handler)
	if len(jwtSecret) != 0 {
		handler = newJWTHandler(jwtSecret, handler)
	}
	return newGzipHandler(handler)
}

type gzipResponseWriter struct {
	resp http.ResponseWriter

	gz            *gzip.Writer
	contentLength uint64 // total length of the uncompressed response
	written       uint64 // amount of written bytes from the uncompressed response
	hasLength     bool   // true if uncompressed response had Content-Length
	inited        bool   // true after init was called for the first time
}

// init runs just before response headers are written. Among other things, this function
// also decides whether compression will be applied at all.
func (w *gzipResponseWriter) init() {
	if w.inited {
		return
	}
	w.inited = true

	hdr := w.resp.Header()
	length := hdr.Get("content-length")
	if len(length) > 0 {
		if n, err := strconv.ParseUint(length, 10, 64); err != nil {
			w.hasLength = true
			w.contentLength = n
		}
	}

	// Setting Transfer-Encoding to "identity" explicitly disables compression. net/http
	// also recognizes this header value and uses it to disable "chunked" transfer
	// encoding, trimming the header from the response. This means downstream handlers can
	// set this without harm, even if they aren't wrapped by newGzipHandler.
	//
	// In go-ethereum, we use this signal to disable compression for certain error
	// responses which are flushed out close to the write deadline of the response. For
	// these cases, we want to avoid chunked transfer encoding and compression because
	// they require additional output that may not get written in time.
	passthrough := hdr.Get("transfer-encoding") == "identity"
	if !passthrough {
		w.gz = gzPool.Get().(*gzip.Writer)
		w.gz.Reset(w.resp)
		hdr.Del("content-length")
		hdr.Set("content-encoding", "gzip")
	}
}

func (w *gzipResponseWriter) Header() http.Header {
	return w.resp.Header()
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	w.init()
	w.resp.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	w.init()

	if w.gz == nil {
		// Compression is disabled.
		return w.resp.Write(b)
	}

	n, err := w.gz.Write(b)
	w.written += uint64(n)
	if w.hasLength && w.written >= w.contentLength {
		// The HTTP handler has finished writing the entire uncompressed response. Close
		// the gzip stream to ensure the footer will be seen by the client in case the
		// response is flushed after this call to write.
		err = w.gz.Close()
	}
	return n, err
}

func (w *gzipResponseWriter) Flush() {
	if w.gz != nil {
		w.gz.Flush()
	}
	if f, ok := w.resp.(http.Flusher); ok {
		f.Flush()
	}
}

var gzPool = sync.Pool{
	New: func() interface{} {
		w := gzip.NewWriter(io.Discard)
		return w
	},
}

func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}
	w.gz.Close()
	gzPool.Put(w.gz)
	w.gz = nil
}

func newGzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		wrapper := &gzipResponseWriter{resp: w}
		defer wrapper.close()

		next.ServeHTTP(wrapper, r)
	})
}

// virtualHostHandler is a handler which validates the Host-header of incoming requests.
// Using virtual hosts can help prevent DNS rebinding attacks, where a 'random' domain name points to
// the service ip address (but without CORS headers). By verifying the targeted virtual host, we can
// ensure that it's a destination that the node operator has defined.
type virtualHostHandler struct {
	vhosts map[string]struct{}
	next   http.Handler
}

// ServeHTTP serves JSON-RPC requests over HTTP, implements http.Handler
func (h *virtualHostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// if r.Host is not set, we can continue serving since a browser would set the Host header
	if r.Host == "" {
		h.next.ServeHTTP(w, r)
		return
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		// Either invalid (too many colons) or no port specified
		host = r.Host
	}
	if ipAddr := net.ParseIP(host); ipAddr != nil {
		// It's an IP address, we can serve that
		h.next.ServeHTTP(w, r)
		return
	}
	// Not an IP address, but a hostname. Need to validate
	if _, exist := h.vhosts["*"]; exist {
		h.next.ServeHTTP(w, r)
		return
	}
	if _, exist := h.vhosts[host]; exist {
		h.next.ServeHTTP(w, r)
		return
	}
	http.Error(w, "invalid host specified", http.StatusForbidden)
}

func newVHostHandler(vhosts []string, next http.Handler) http.Handler {
	vhostMap := make(map[string]struct{})
	for _, allowedHost := range vhosts {
		vhostMap[strings.ToLower(allowedHost)] = struct{}{}
	}
	return &virtualHostHandler{vhostMap, next}
}

func newCorsHandler(srv http.Handler, allowedOrigins []string) http.Handler {
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

// enableWS turns on JSON-RPC over WebSocket on the server.
func (h *httpServer) EnableWS(apis []rpc.API, config WSConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

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
	h.wsConfig = config
	h.wsHandler.Store(&rpcHandler{
		Handler: NewWSHandlerStack(srv.WebsocketHandler(config.Origins), config.jwtSecret),
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

// stop shuts down the HTTP server.
func (h *httpServer) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.doStop()
}

func (h *httpServer) doStop() {
	if h.listener == nil {
		return // not running
	}

	// Shut down the server.
	httpHandler := h.httpHandler.Load().(*rpcHandler)
	wsHandler := h.wsHandler.Load().(*rpcHandler)
	if httpHandler != nil {
		h.httpHandler.Store((*rpcHandler)(nil))
		httpHandler.server.Stop()
	}
	if wsHandler != nil {
		h.wsHandler.Store((*rpcHandler)(nil))
		wsHandler.server.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := h.server.Shutdown(ctx)
	if err != nil && err == ctx.Err() {
		log.Warn().Msg("HTTP server graceful shutdown timed out")
		h.server.Close()
	}

	h.listener.Close()
	log.Info().Msg(
		fmt.Sprint("HTTP server stopped", "endpoint", h.listener.Addr()),
	)

	// Clear out everything to allow re-configuring it later.
	h.host, h.port, h.endpoint = "", 0, ""
	h.server, h.listener = nil, nil
}

// stopWS disables JSON-RPC over WebSocket and also stops the server if it only serves WebSocket.
func (h *httpServer) stopWS() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.disableWS() {
		if !h.rpcAllowed() {
			h.doStop()
		}
	}
}

const jwtExpiryTimeout = 60 * time.Second

// NewWSHandlerStack returns a wrapped ws-related handler.
func NewWSHandlerStack(srv http.Handler, jwtSecret []byte) http.Handler {
	if len(jwtSecret) != 0 {
		return newJWTHandler(jwtSecret, srv)
	}
	return srv
}

// newJWTHandler creates a http.Handler with jwt authentication support.
func newJWTHandler(secret []byte, next http.Handler) http.Handler {
	return &jwtHandler{
		keyFunc: func(token *jwt.Token) (interface{}, error) {
			return secret, nil
		},
		next: next,
	}
}

type jwtHandler struct {
	keyFunc func(token *jwt.Token) (interface{}, error)
	next    http.Handler
}

// ServeHTTP implements http.Handler
func (handler *jwtHandler) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	var (
		strToken string
		claims   jwt.RegisteredClaims
	)
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		strToken = strings.TrimPrefix(auth, "Bearer ")
	}
	if len(strToken) == 0 {
		http.Error(out, "missing token", http.StatusUnauthorized)
		return
	}
	// We explicitly set only HS256 allowed, and also disables the
	// claim-check: the RegisteredClaims internally requires 'iat' to
	// be no later than 'now', but we allow for a bit of drift.
	token, err := jwt.ParseWithClaims(strToken, &claims, handler.keyFunc,
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithoutClaimsValidation())

	switch {
	case err != nil:
		http.Error(out, err.Error(), http.StatusUnauthorized)
	case !token.Valid:
		http.Error(out, "invalid token", http.StatusUnauthorized)
	case !claims.VerifyExpiresAt(time.Now(), false): // optional
		http.Error(out, "token is expired", http.StatusUnauthorized)
	case claims.IssuedAt == nil:
		http.Error(out, "missing issued-at", http.StatusUnauthorized)
	case time.Since(claims.IssuedAt.Time) > jwtExpiryTimeout:
		http.Error(out, "stale token", http.StatusUnauthorized)
	case time.Until(claims.IssuedAt.Time) > jwtExpiryTimeout:
		http.Error(out, "future token", http.StatusUnauthorized)
	default:
		handler.next.ServeHTTP(out, r)
	}
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
