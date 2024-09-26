package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strconv"

	"github.com/rs/zerolog"
)

type ProfileServer struct {
	logger   zerolog.Logger
	server   *http.Server
	endpoint string
}

func NewProfileServer(
	logger zerolog.Logger,
	host string,
	port int,
) *ProfileServer {
	endpoint := net.JoinHostPort(host, strconv.Itoa(port))
	return &ProfileServer{
		logger:   logger,
		server:   &http.Server{Addr: endpoint},
		endpoint: endpoint,
	}
}

func (h *ProfileServer) ListenAddr() string {
	return h.endpoint
}

func (s *ProfileServer) Start() {
	go func() {
		err := s.server.ListenAndServe()
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				s.logger.Warn().Msg("Profiler server shutdown")
				return
			}
			s.logger.Err(err).Msg("failed to start Profiler server")
			panic(err)
		}
	}()
}

func (s *ProfileServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return s.server.Shutdown(ctx)
}

func (s *ProfileServer) Close() error {
	return s.server.Close()
}
