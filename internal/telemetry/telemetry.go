// Package telemetry sends anonymous OSS usage pings.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const endpoint = "https://oss.raczylo.com/v1/ping"

type pingPayload struct {
	Project string `json:"project"`
	Version string `json:"version"`
	Ts      int64  `json:"ts"`
}

// Ping fires an anonymous analytics ping asynchronously. Never blocks the caller.
// Silently skips dev/dirty builds; all errors are logged at debug level only.
func Ping(project, version string) {
	if version == "dev" || strings.Contains(version, "dirty") {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		body, err := json.Marshal(pingPayload{
			Project: project,
			Version: strings.TrimPrefix(version, "v"),
			Ts:      time.Now().Unix(),
		})
		if err != nil {
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Debug().Err(err).Msg("telemetry ping failed")
			return
		}
		resp.Body.Close()
	}()
}
