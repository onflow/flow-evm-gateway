package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/onflow/flow-evm-gateway/eth"
)

// HttpHandler is a thin middleware for gathering metrics about http request.
// It makes no decision about error handling. If one occurred, we log it and
// pass request on to the underlying handler to make a decision
type HttpHandler struct {
	handler   http.Handler
	collector Collector
	logger    zerolog.Logger
}

func NewMetricsHandler(handler http.Handler, collector Collector, log zerolog.Logger) *HttpHandler {
	return &HttpHandler{
		handler:   handler,
		collector: collector,
		logger:    log,
	}
}

func (h *HttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var start time.Time

	method, err := extractMethod(r, h.logger)
	if err != nil {
		h.logger.Debug().Err(err).Msg("error extracting method")
	} else if eth.IsValidMethod(method) {
		start = time.Now()
		defer h.collector.MeasureRequestDuration(start, method)
	}

	h.handler.ServeHTTP(w, r)
}

func extractMethod(r *http.Request, logger zerolog.Logger) (string, error) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error().Msgf("failed to extract method: %v", r)
		}
	}()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", fmt.Errorf("error reading request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var requestBody struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &requestBody); err != nil {
		return "", fmt.Errorf("error extracting method from body: %w", err)
	}

	return requestBody.Method, nil
}
