package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// HttpHandler is a thin middleware for gathering metrics about http request.
// It makes no decision about error handling. If one occurred, we log it and
// pass request on to the underlying handler to make a decision
type HttpHandler struct {
	handler   http.Handler
	collector Collector
	logger    zerolog.Logger
}

func NewHttpHandler(handler http.Handler, collector Collector, logger zerolog.Logger) *HttpHandler {
	return &HttpHandler{
		handler:   handler,
		collector: collector,
		logger:    logger,
	}
}

func (h *HttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var start time.Time

	method, err := extractMethod(r)
	if err != nil {
		h.logger.Debug().Err(err).Msg("no metrics will be collected. error extracting method: ")
	} else {
		start = time.Now()
		defer h.collector.MeasureRequestDuration(start, method)
	}

	h.handler.ServeHTTP(w, r)
}

func extractMethod(r *http.Request) (string, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", fmt.Errorf("error reading request body: %s", err)
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var requestBody struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &requestBody); err != nil {
		return "", fmt.Errorf("error extracting method from body: %s", err)
	}

	return requestBody.Method, nil
}
