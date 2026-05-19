package api

import (
	"net/http"

	"github.com/mattjackson/basement/internal/driver"
)

// listBucketsHandler handles GET /api/v1/admin/buckets.
// Calls driver.ListBuckets and returns JSON []Bucket per OpenAPI schema.
func (s *Server) listBucketsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	buckets, err := s.drv.ListBuckets(r.Context())
	if err != nil {
		writeDriverError(w, "ListBuckets", err)
		return
	}

	if buckets == nil {
		buckets = []driver.Bucket{}
	}

	writeJSON(w, http.StatusOK, buckets)
}
