package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/futurebuildai/ai-lm/pkg/database"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a workflow plan does not exist.
var ErrNotFound = errors.New("workflow plan not found")

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

// payload is everything outside the dedicated columns, stored as one JSONB doc.
type payload struct {
	DepotLat         float64         `json:"depot_lat"`
	DepotLng         float64         `json:"depot_lng"`
	Orders           []OrderAnalysis `json:"orders"`
	Loads            []TruckLoad     `json:"loads"`
	UnassignedOrders []Stop          `json:"unassigned_orders"`
}

func (r *Repository) marshalPayload(p *Plan) ([]byte, error) {
	return json.Marshal(payload{
		DepotLat:         p.DepotLat,
		DepotLng:         p.DepotLng,
		Orders:           p.Orders,
		Loads:            p.Loads,
		UnassignedOrders: p.UnassignedOrders,
	})
}

func (r *Repository) unmarshalPayload(raw []byte, p *Plan) error {
	var pl payload
	if err := json.Unmarshal(raw, &pl); err != nil {
		return fmt.Errorf("unmarshal workflow payload: %w", err)
	}
	p.DepotLat = pl.DepotLat
	p.DepotLng = pl.DepotLng
	p.Orders = pl.Orders
	p.Loads = pl.Loads
	p.UnassignedOrders = pl.UnassignedOrders
	if p.Orders == nil {
		p.Orders = []OrderAnalysis{}
	}
	if p.Loads == nil {
		p.Loads = []TruckLoad{}
	}
	if p.UnassignedOrders == nil {
		p.UnassignedOrders = []Stop{}
	}
	return nil
}

// Create inserts a new plan and assigns id/timestamps.
func (r *Repository) Create(ctx context.Context, p *Plan) error {
	raw, err := r.marshalPayload(p)
	if err != nil {
		return fmt.Errorf("marshal workflow payload: %w", err)
	}
	err = r.db.GetExecutor(ctx).QueryRow(ctx, `
		INSERT INTO workflow_plans (plan_date, status, payload)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`,
		p.PlanDate, p.Status, raw).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert workflow_plan: %w", err)
	}
	return nil
}

// Update persists the current state of an existing plan.
func (r *Repository) Update(ctx context.Context, p *Plan) error {
	raw, err := r.marshalPayload(p)
	if err != nil {
		return fmt.Errorf("marshal workflow payload: %w", err)
	}
	tag, err := r.db.GetExecutor(ctx).Exec(ctx, `
		UPDATE workflow_plans SET status=$2, payload=$3, updated_at=NOW() WHERE id=$1`,
		p.ID, p.Status, raw)
	if err != nil {
		return fmt.Errorf("update workflow_plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns one plan by id.
func (r *Repository) Get(ctx context.Context, id string) (*Plan, error) {
	var p Plan
	var raw []byte
	err := r.db.GetExecutor(ctx).QueryRow(ctx, `
		SELECT id, plan_date::text, status, payload, created_at, updated_at
		FROM workflow_plans WHERE id=$1`, id).
		Scan(&p.ID, &p.PlanDate, &p.Status, &raw, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query workflow_plan: %w", err)
	}
	if err := r.unmarshalPayload(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetLatestForDate returns the most recent plan for a date, or ErrNotFound.
func (r *Repository) GetLatestForDate(ctx context.Context, date string) (*Plan, error) {
	var p Plan
	var raw []byte
	err := r.db.GetExecutor(ctx).QueryRow(ctx, `
		SELECT id, plan_date::text, status, payload, created_at, updated_at
		FROM workflow_plans WHERE plan_date=$1
		ORDER BY created_at DESC LIMIT 1`, date).
		Scan(&p.ID, &p.PlanDate, &p.Status, &raw, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query workflow_plan by date: %w", err)
	}
	if err := r.unmarshalPayload(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
