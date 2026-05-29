package catalog

import "context"

// Service holds product-dimension business logic.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// ListDimensions returns all product dimension records.
func (s *Service) ListDimensions(ctx context.Context) ([]Dimension, error) {
	return s.repo.List(ctx)
}

// GetDimension returns dimensions for one product, or ErrNotFound.
func (s *Service) GetDimension(ctx context.Context, gableProductID string) (*Dimension, error) {
	return s.repo.GetByProductID(ctx, gableProductID)
}

// UpsertDimension creates or replaces a product's dimensions.
func (s *Service) UpsertDimension(ctx context.Context, gableProductID string, in DimensionInput) (*Dimension, error) {
	return s.repo.Upsert(ctx, gableProductID, in)
}
