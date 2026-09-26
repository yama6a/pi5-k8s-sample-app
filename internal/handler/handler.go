package handler

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/yama6a/cluster-sampleapp/internal/audit"
	"github.com/yama6a/cluster-sampleapp/internal/store"
)

type Server struct {
	store  *store.Store
	audit  *audit.Store
	logger *zap.Logger
}

func NewServer(s *store.Store, auditStore *audit.Store, logger *zap.Logger) *Server {
	return &Server{store: s, audit: auditStore, logger: logger}
}

// ListUsers serves GET /users: every stored user, oldest first.
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

// ListAudit serves GET /audit: the audit events of recently active users, keyed by user UUID.
func (s *Server) ListAudit(w http.ResponseWriter, r *http.Request) {
	events, err := s.audit.ListAll(r.Context())
	if err != nil {
		s.logger.Error("list audit", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(events); err != nil {
		s.logger.Error("encode audit", zap.Error(err))
	}
}
