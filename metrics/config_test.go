package metrics

import (
	"os"
	"testing"
)

func TestReadPortFromConfigFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected uint
		wantErr  bool
	}{
		{
			name: "Valid config",
			content: `
scrape_configs:
  - job_name: flow-evm-gateway
    static_configs:
      - targets:
          - localhost:9091
`,
			expected: 9091,
			wantErr:  false,
		},
		{
			name:     "Empty file",
			content:  "",
			expected: 0,
			wantErr:  true,
		},
		{
			name: "No scrape configs",
			content: `
scrape_configs:
`,
			expected: 0,
			wantErr:  true,
		},
		{
			name: "No static configs",
			content: `
scrape_configs:
  - job_name: flow-evm-gateway
`,
			expected: 0,
			wantErr:  true,
		},
		{
			name: "No targets",
			content: `
scrape_configs:
  - job_name: flow-evm-gateway
    static_configs:
      - targets: []
`,
			expected: 0,
			wantErr:  true,
		},
		{
			name: "Invalid target",
			content: `
scrape_configs:
  - job_name: flow-evm-gateway
    static_configs:
      - targets:
          - localhost
`,
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile, err := os.CreateTemp("", "test-config-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpfile.Name())

			if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			if err := tmpfile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			port, err := readPortFromConfigFile(tmpfile.Name())
			isError := err != nil
			if isError != tt.wantErr {
				t.Errorf("readPortFromConfigFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if port != tt.expected {
				t.Errorf("readPortFromConfigFile() = %v, want %v", port, tt.expected)
			}
		})
	}
}

func TestExtractPortFromTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		expected uint
		wantErr  bool
	}{
		{"Valid target", "localhost:9091", 9091, false},
		{"Invalid format", "localhost", 0, true},
		{"Invalid port", "localhost:abc", 0, true},
		{"Port out of range (low)", "localhost:0", 0, true},
		{"Port out of range (high)", "localhost:65536", 0, true},
		{"Valid IP:port", "127.0.0.1:8080", 8080, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := extractPortFromTarget(tt.target)
			isError := err != nil
			if isError != tt.wantErr {
				t.Errorf("extractPortFromTarget() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if port != tt.expected {
				t.Errorf("extractPortFromTarget() = %v, want %v", port, tt.expected)
			}
		})
	}
}
