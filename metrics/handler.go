package metrics

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		errMsg := fmt.Sprintf("error reading request body: %s", err)
		http.Error(w, errMsg, http.StatusBadRequest)
		return "", err
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var requestBody struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &requestBody); err != nil {
		errMsg := fmt.Sprintf("error extracting method from body: %s", err)
		http.Error(w, errMsg, http.StatusBadRequest)
		return "", err
	}

	return requestBody.Method, nil
}
