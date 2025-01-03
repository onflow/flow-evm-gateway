package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/onflow/flow-go/module/component"
	"github.com/onflow/flow-go/module/irrecoverable"

	"github.com/rs/zerolog"
)

type ProfileServer struct {
	component.Component

	log      zerolog.Logger
	server   *http.Server
	endpoint string
}

var _ component.Component = (*ProfileServer)(nil)

func NewProfileServer(
	logger zerolog.Logger,
	host string,
	port int,
) *ProfileServer {
	endpoint := net.JoinHostPort(host, strconv.Itoa(port))

	s := &ProfileServer{
		log:      logger,
		server:   &http.Server{Addr: endpoint},
		endpoint: endpoint,
	}

	s.Component = component.NewComponentManagerBuilder().
		AddWorker(s.serve).
		AddWorker(s.shutdownOnContextDone).
		Build()

	return s
}

func (s *ProfileServer) serve(ctx irrecoverable.SignalerContext, ready component.ReadyFunc) {
	s.log.Info().Msg("starting profiler server on address")

	l, err := net.Listen("tcp", s.endpoint)
	if err != nil {
		s.log.Err(err).Msg("failed to start the metrics server")
		ctx.Throw(err)
		return
	}

	ready()

	// pass the signaler context to the server so that the signaler context
	// can control the server's lifetime
	s.server.BaseContext = func(_ net.Listener) context.Context {
		return ctx
	}

	err = s.server.Serve(l) // blocking call
	if err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return
		}

		log.Err(err).Msg("fatal error in the metrics server")
		ctx.Throw(err)
	}
}

func (s *ProfileServer) shutdownOnContextDone(ictx irrecoverable.SignalerContext, ready component.ReadyFunc) {
	ready()
	<-ictx.Done()

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
}
