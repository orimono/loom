package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/orimono/loom/internal/store"
)

func HistoryHandler(ts *store.TelemetryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		typ := r.URL.Query().Get("type")
		if nodeID == "" || typ == "" {
			http.Error(w, "node_id and type required", http.StatusBadRequest)
			return
		}

		limit := 120
		if l := r.URL.Query().Get("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1440 {
				limit = v
			}
		}

		records, err := ts.Query(r.Context(), nodeID, typ, limit)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}


		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(records)
	}
}
