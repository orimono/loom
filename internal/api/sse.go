package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/orimono/loom/internal/telemetry"
)

func SSEHandler(hub *telemetry.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		typ := r.URL.Query().Get("type")
		if nodeID == "" || typ == "" {
			http.Error(w, "node_id and type required", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch, cancel := hub.Subscribe(telemetry.SubKey{NodeID: nodeID, Type: typ})
		defer cancel()

		for {
			select {
			case <-r.Context().Done():
				return
			case t, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(t)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
