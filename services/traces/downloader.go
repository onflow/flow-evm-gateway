package traces

import (
	"encoding/json"
)

type Downloader interface {
	// Download traces or returning an error with the failure
	Download(id string) (json.RawMessage, error)
}

var _ Downloader = &GCPDownloader{}

type GCPDownloader struct {
	bucketName string
}

func (G *GCPDownloader) Download(id string) (json.RawMessage, error) {
	return nil, nil
}
