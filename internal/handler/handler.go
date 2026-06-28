package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/internal/store"
)

type Server struct {
	store  *store.Store
	logger *zap.Logger
}

func NewServer(s *store.Store, logger *zap.Logger) *Server {
	return &Server{store: s, logger: logger}
}

// GetHeaders implements the generated api.ServerInterface: it echoes every
// request header back as plain text, followed by the bootstrap timestamp.
func (s *Server) GetHeaders(w http.ResponseWriter, r *http.Request) {
	bootstrapped, err := s.store.BootstrapTime(r.Context())
	if err != nil {
		s.logger.Error("read bootstrap time", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, strings.Join(r.Header[k], ", "))
	}
	fmt.Fprintf(&b, "\nSample App Bootstrapped At: %s\n", bootstrapped.UTC().Format(time.RFC3339Nano))

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, b.String())
}
