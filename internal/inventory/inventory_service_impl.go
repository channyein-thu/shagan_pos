package inventory

import "context"

type Service struct {
	repo     Repository
	branches BranchLookup
}

func NewService(repo Repository, branches BranchLookup) *Service {
	return &Service{repo: repo, branches: branches}
}

var _ Interface = (*Service)(nil)

// ListStockLevels resolves branchID (see the interface doc) into the set of
// branch IDs to query - one verified branch, or every branch in orgID -
// before delegating to the repository, since StockLevel has no OrgID
// column of its own to filter by directly.
func (s *Service) ListStockLevels(ctx context.Context, orgID uint, branchID *uint, productID *uint) ([]StockLevel, error) {
	var branchIDs []uint
	if branchID != nil {
		if _, err := s.branches.GetBranch(ctx, orgID, *branchID); err != nil {
			return nil, err
		}
		branchIDs = []uint{*branchID}
	} else {
		branches, err := s.branches.ListBranches(ctx, orgID)
		if err != nil {
			return nil, err
		}
		branchIDs = make([]uint, len(branches))
		for i, b := range branches {
			branchIDs[i] = b.ID
		}
	}

	return s.repo.ListStockLevels(ctx, branchIDs, productID)
}

func (s *Service) ListLowStock(ctx context.Context) ([]StockLevel, error) {
	return s.repo.ListLowStock(ctx)
}

func (s *Service) ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error) {
	return s.repo.ListInventoryLedger(ctx)
}

func (s *Service) CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error) {
	return s.repo.CreateStockAdjustment(ctx, in)
}

func (s *Service) ListStockTransfers(ctx context.Context) ([]StockTransfer, error) {
	return s.repo.ListStockTransfers(ctx)
}

func (s *Service) CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error) {
	return s.repo.CreateStockTransfer(ctx, in)
}

func (s *Service) UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error) {
	return s.repo.UpdateStockTransfer(ctx, id, in)
}
