package api

import (
	"encoding/json"
	"net/http"

	"github.com/orimono/loom/internal/node"
)

type NodeResponse struct {
	NodeID     string      `json:"node_id"`
	Hostname   string      `json:"hostname"`
	OS         string      `json:"os"`
	Arch       string      `json:"arch"`
	Tags       []string    `json:"tags"`
	Status     node.Status `json:"status"`
	LastSeenAt string      `json:"last_seen_at"`
}

func NodesHandler(registry *node.NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes := registry.ListAll()

		resp := make([]NodeResponse, 0, len(nodes))
		for _, n := range nodes {
			resp = append(resp, NodeResponse{
				NodeID:     n.NodeID,
				Hostname:   n.Hostname,
				OS:         n.OS,
				Arch:       n.Arch,
				Tags:       n.Tags,
				Status:     n.Status,
				LastSeenAt: n.LastSeenAt.Format("2006-01-02 15:04:05"),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(resp)
	}
}
