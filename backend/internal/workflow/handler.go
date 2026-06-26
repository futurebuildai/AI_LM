package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/futurebuildai/ai-lm/pkg/httputil"
)

// Handler exposes the guided workflow REST endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers workflow routes. roleGuard protects writes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, roleGuard ...func(http.Handler) http.Handler) {
	guard := func(handler http.HandlerFunc) http.HandlerFunc {
		if len(roleGuard) > 0 && roleGuard[0] != nil {
			return func(w http.ResponseWriter, r *http.Request) {
				roleGuard[0](handler).ServeHTTP(w, r)
			}
		}
		return handler
	}

	mux.HandleFunc("POST /api/v1/workflow/plans", guard(h.HandleIngest))
	mux.HandleFunc("GET /api/v1/workflow/plans/latest", guard(h.HandleLatest))
	mux.HandleFunc("GET /api/v1/workflow/plans/{id}", guard(h.HandleGet))
	mux.HandleFunc("POST /api/v1/workflow/plans/{id}/assign", guard(h.HandleAssign))
	mux.HandleFunc("POST /api/v1/workflow/plans/{id}/pack", guard(h.HandlePack))
	mux.HandleFunc("PUT /api/v1/workflow/plans/{id}/loads/{vehicleId}/sequence", guard(h.HandleResequence))
	mux.HandleFunc("POST /api/v1/workflow/plans/{id}/review", guard(h.HandleReview))
	mux.HandleFunc("POST /api/v1/workflow/plans/{id}/push", guard(h.HandlePush))
}

func (h *Handler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, r, "invalid request body", http.StatusBadRequest, err)
		return
	}
	plan, err := h.svc.Ingest(r.Context(), req)
	if err != nil {
		httputil.RespondError(w, r, "ingest failed", http.StatusBadGateway, err)
		return
	}
	httputil.RespondJSON(w, http.StatusCreated, plan)
}

func (h *Handler) HandleLatest(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		httputil.RespondError(w, r, "date query parameter required", http.StatusBadRequest, nil)
		return
	}
	plan, err := h.svc.GetLatestForDate(r.Context(), date)
	if errors.Is(err, ErrNotFound) {
		httputil.RespondError(w, r, "no plan for date", http.StatusNotFound, err)
		return
	}
	if err != nil {
		httputil.RespondError(w, r, "lookup failed", http.StatusInternalServerError, err)
		return
	}
	httputil.RespondJSON(w, http.StatusOK, plan)
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	plan, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		httputil.RespondError(w, r, "plan not found", http.StatusNotFound, err)
		return
	}
	if err != nil {
		httputil.RespondError(w, r, "lookup failed", http.StatusInternalServerError, err)
		return
	}
	httputil.RespondJSON(w, http.StatusOK, plan)
}

func (h *Handler) HandleAssign(w http.ResponseWriter, r *http.Request) {
	h.step(w, r, h.svc.Assign)
}

func (h *Handler) HandlePack(w http.ResponseWriter, r *http.Request) {
	h.step(w, r, h.svc.Pack)
}

func (h *Handler) HandleReview(w http.ResponseWriter, r *http.Request) {
	h.step(w, r, h.svc.Review)
}

func (h *Handler) HandlePush(w http.ResponseWriter, r *http.Request) {
	h.step(w, r, h.svc.Push)
}

// step runs one id-keyed workflow transition with shared error mapping.
func (h *Handler) step(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id string) (*Plan, error)) {
	plan, err := fn(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		httputil.RespondError(w, r, "plan not found", http.StatusNotFound, err)
		return
	}
	if err != nil {
		httputil.RespondError(w, r, "workflow step failed", http.StatusUnprocessableEntity, err)
		return
	}
	httputil.RespondJSON(w, http.StatusOK, plan)
}

func (h *Handler) HandleResequence(w http.ResponseWriter, r *http.Request) {
	var req ResequenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, r, "invalid request body", http.StatusBadRequest, err)
		return
	}
	plan, err := h.svc.Resequence(r.Context(), r.PathValue("id"), r.PathValue("vehicleId"), req.OrderIDs)
	if errors.Is(err, ErrNotFound) {
		httputil.RespondError(w, r, "plan not found", http.StatusNotFound, err)
		return
	}
	if err != nil {
		httputil.RespondError(w, r, "resequence failed", http.StatusUnprocessableEntity, err)
		return
	}
	httputil.RespondJSON(w, http.StatusOK, plan)
}
