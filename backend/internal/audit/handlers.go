package audit

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpx"
)

const (
	defaultLimit = 100
	maxLimit     = 500
)

type logDTO struct {
	ID         uint64          `json:"id"`
	UserID     *uint64         `json:"user_id,omitempty"`
	UserName   *string         `json:"user_name,omitempty"`
	UserEmail  *string         `json:"user_email,omitempty"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   *uint64         `json:"entity_id,omitempty"`
	Detail     json.RawMessage `json:"detail"`
	IPAddress  *string         `json:"ip_address,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

func toLogDTO(row db.ListAuditLogsRow) logDTO {
	dto := logDTO{
		ID:         row.ID,
		Action:     row.Action,
		EntityType: row.EntityType,
		Detail:     row.Detail,
		CreatedAt:  row.CreatedAt.Format(time.RFC3339),
	}
	if row.UserID.Valid {
		id := uint64(row.UserID.Int64)
		dto.UserID = &id
	}
	if row.UserName.Valid {
		dto.UserName = &row.UserName.String
	}
	if row.UserEmail.Valid {
		dto.UserEmail = &row.UserEmail.String
	}
	if row.EntityID.Valid {
		id := uint64(row.EntityID.Int64)
		dto.EntityID = &id
	}
	if row.IpAddress.Valid {
		dto.IPAddress = &row.IpAddress.String
	}
	return dto
}

func (s *Service) ListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := ListFilter{
		Action:     q.Get("action"),
		EntityType: q.Get("entity_type"),
		Limit:      defaultLimit,
	}
	if v := q.Get("user_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid user_id", http.StatusBadRequest)
			return
		}
		filter.UserID = &id
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid from (expected RFC3339)", http.StatusBadRequest)
			return
		}
		filter.FromDate = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid to (expected RFC3339)", http.StatusBadRequest)
			return
		}
		filter.ToDate = &t
	}
	if v := q.Get("limit"); v != "" {
		limit, err := strconv.Atoi(v)
		if err != nil || limit <= 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		filter.Limit = int32(limit)
	}
	if filter.Limit > maxLimit {
		filter.Limit = maxLimit
	}
	if v := q.Get("offset"); v != "" {
		offset, err := strconv.Atoi(v)
		if err != nil || offset < 0 {
			http.Error(w, "invalid offset", http.StatusBadRequest)
			return
		}
		filter.Offset = int32(offset)
	}

	rows, err := s.List(r.Context(), filter)
	if err != nil {
		slog.Error("list audit logs failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]logDTO, len(rows))
	for i, row := range rows {
		out[i] = toLogDTO(row)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
