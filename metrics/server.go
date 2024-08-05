package metrics

import (
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

// endpoint where metrics are available for scraping
const endpoint = "/metrics"

// Server is the http server that will be serving metrics requests
type Server struct {
	server *http.Server
	log    zerolog.Logger
}

// NewServer creates a new server that will start on the specified port,
// and responds to only the `/metrics` endpoint
func NewServer(log zerolog.Logger, port int) *Server {
	log = log.With().Str("component", "metrics-server").Logger()
	addr := fmt.Sprintf(":%d", port)

	mux := http.NewServeMux()
	mux.Handle(endpoint, promhttp.Handler())

	return &Server{
		server: &http.Server{Addr: addr, Handler: mux},
		log:    log,
	}
}

// Start starts the server and returns a channel which is closed
// when the server is ready to serve requests.
func (s *Server) Start() (<-chan struct{}, error) {
	ready := make(chan struct{})
	defer close(ready)

	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		s.log.Err(err).Msg("error listening on address")
		return nil, err
	}

	go func() {
		err := s.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Err(err).Msg("error serving metrics server")
		}
	}()

	return ready, nil
}
