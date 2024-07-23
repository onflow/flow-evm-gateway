package metrics

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type HttpHandler struct {
	handler   http.Handler
	collector Collector
}

func NewHttpHandler(handler http.Handler, collector Collector) *HttpHandler {
	return &HttpHandler{
		handler:   handler,
		collector: collector,
	}
}

func (h *HttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	method, err := extractMethod(w, r)
	if err != nil {
		return
	}

	defer h.collector.MeasureRequestDuration(start, prometheus.Labels{"method": method})
	h.handler.ServeHTTP(w, r)
}

func extractMethod(w http.ResponseWriter, r *http.Request) (string, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return "", err
	}

	var requestBody struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &requestBody); err != nil {
		http.Error(w, "Error extracting method field from body", http.StatusBadRequest)
		return "", err
	}

	return requestBody.Method, nil
}
