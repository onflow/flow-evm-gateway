package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	"time"

	"github.com/onflow/flow-go/module/component"
	"github.com/onflow/flow-go/module/irrecoverable"

	"github.com/rs/zerolog"
)

type ProfileServer struct {
	log      zerolog.Logger
	server   *http.Server
	endpoint string

	startupCompleted chan struct{}
}

var _ component.Component = (*ProfileServer)(nil)

func NewProfileServer(
	logger zerolog.Logger,
	host string,
	port int,
) *ProfileServer {
	endpoint := net.JoinHostPort(host, strconv.Itoa(port))
	return &ProfileServer{
		log:              logger,
		server:           &http.Server{Addr: endpoint},
		endpoint:         endpoint,
		startupCompleted: make(chan struct{}),
	}
}

func (s *ProfileServer) Start(ctx irrecoverable.SignalerContext) {
	defer close(s.startupCompleted)

	s.server.BaseContext = func(_ net.Listener) context.Context {
		return ctx
	}

	go func() {
		s.log.Info().Msgf("Profiler server started: %s", s.endpoint)

		if err := s.server.ListenAndServe(); err != nil {
			// http.ErrServerClosed is returned when Close or Shutdown is called
			// we don't consider this an error, so print this with debug level instead
			if errors.Is(err, http.ErrServerClosed) {
				s.log.Debug().Err(err).Msg("Profiler server shutdown")
			} else {
				s.log.Err(err).Msg("error running profiler server")
			}
		}
	}()
}

func (s *ProfileServer) Ready() <-chan struct{} {
	ready := make(chan struct{})

	go func() {
		<-s.startupCompleted
		close(ready)
	}()

	return ready
}

func (s *ProfileServer) Done() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		<-s.startupCompleted
		defer close(done)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := s.server.Shutdown(ctx)
		if err == nil {
			s.log.Info().Msg("Profiler server graceful shutdown completed")
		}

		if errors.Is(err, ctx.Err()) {
			s.log.Warn().Msg("Profiler server graceful shutdown timed out")
			err := s.server.Close()
			if err != nil {
				s.log.Err(err).Msg("error closing profiler server")
			}
		} else {
			s.log.Err(err).Msg("error shutting down profiler server")
		}
	}()
	return done
}
