package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

// Server is the http server that will be serving the /metrics request for prometheus
type Server struct {
	server *http.Server
	log    zerolog.Logger
}

// NewServer creates a new server that will start on the specified port,
// and responds to only the `/metrics` endpoint
func NewServer(log zerolog.Logger, prometheusConfigPath string) (*Server, error) {
	port, err := readPortFromConfigFile(prometheusConfigPath)
	if err != nil {
		log.Warn().Err(err).Msgf("could not read port from prometheus config file: %s", prometheusConfigPath)
		return nil, err
	}

	mux := http.NewServeMux()
	endpoint := "/metrics"
	mux.Handle(endpoint, promhttp.Handler())

	addr := fmt.Sprintf(":%d", port)
	server := &Server{
		server: &http.Server{Addr: addr, Handler: mux},
		log:    log,
	}

	log.Info().Str("address", addr).Str("endpoint", endpoint).Msg("metrics server started")

	return server, nil
}

func (s *Server) Ready() <-chan struct{} {
	ready := make(chan struct{})

	go func() {
		listener, err := net.Listen("tcp", s.server.Addr)
		if err != nil {
			s.log.Err(err).Msg("error listening on address")
			close(ready)
			return
		}

		close(ready)
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Err(err).Msg("error serving metrics server")
		}
	}()

	return ready
}

func (s *Server) Done() <-chan struct{} {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	go func() {
		defer cancel()
		if err := s.server.Shutdown(ctx); err != nil {
			s.log.Err(err).Msg("error shutting down metrics server")
		}
	}()

	return ctx.Done()
}
