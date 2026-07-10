package handler

import (
	"encoding/json"
	"net/http"

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

// ListUsers implements the generated api.ServerInterface: it returns every persisted user as a
// JSON array of {id, createdAt}, oldest first. store.User's JSON tags match the OpenAPI schema.
func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.logger.Error("list users", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(users); err != nil {
		s.logger.Error("encode users", zap.Error(err))
	}
}
