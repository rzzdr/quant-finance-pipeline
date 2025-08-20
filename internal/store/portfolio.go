package store

import (
	"sync"

	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/errors"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// InMemoryPortfolioStore implements an in-memory portfolio storage
type InMemoryPortfolioStore struct {
	portfolios map[string]*models.Portfolio
	mu         sync.RWMutex
	log        *logger.Logger
}

// NewInMemoryPortfolioStore creates a new in-memory portfolio store
func NewInMemoryPortfolioStore() *InMemoryPortfolioStore {
	return &InMemoryPortfolioStore{
		portfolios: make(map[string]*models.Portfolio),
		log:        logger.GetLogger("store.portfolio"),
	}
}

// GetPortfolio retrieves a portfolio by ID
func (s *InMemoryPortfolioStore) GetPortfolio(id string) (*models.Portfolio, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	portfolio, exists := s.portfolios[id]
	if !exists {
		return nil, errors.NotFound("portfolio not found: " + id)
	}

	return portfolio, nil
}

// GetAllPortfolios returns all stored portfolios
func (s *InMemoryPortfolioStore) GetAllPortfolios() ([]*models.Portfolio, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	portfolios := make([]*models.Portfolio, 0, len(s.portfolios))
	for _, p := range s.portfolios {
		portfolios = append(portfolios, p)
	}

	return portfolios, nil
}

// SavePortfolio saves or updates a portfolio
func (s *InMemoryPortfolioStore) SavePortfolio(portfolio *models.Portfolio) error {
	if portfolio == nil {
		return errors.InvalidArgument("cannot save nil portfolio")
	}

	if portfolio.ID == "" {
		return errors.InvalidArgument("portfolio ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.portfolios[portfolio.ID] = portfolio
	return nil
}

// DeletePortfolio removes a portfolio by ID
func (s *InMemoryPortfolioStore) DeletePortfolio(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.portfolios[id]; !exists {
		return errors.NotFound("portfolio not found: " + id)
	}

	delete(s.portfolios, id)
	return nil
}