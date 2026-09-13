package identity

import "context"

// Interface defines the identity domain's use cases.
type Interface interface {
	Login(ctx context.Context) (*Session, error)
	RefreshSession(ctx context.Context) (*Session, error)
	Logout(ctx context.Context) error
	GetMe(ctx context.Context) (*User, error)
	UpdateMe(ctx context.Context, in User) (*User, error)
	VerifyManagerPIN(ctx context.Context) (*Staff, error)
	VerifyStaffPIN(ctx context.Context, id uint) (*Staff, error)
	RegisterDevice(ctx context.Context, in Device) (*Device, error)
	ListDevices(ctx context.Context) ([]Device, error)
	UpdateDevice(ctx context.Context, id uint, in Device) (*Device, error)
	ListBranches(ctx context.Context) ([]Branch, error)
	CreateBranch(ctx context.Context, in Branch) (*Branch, error)
	GetBranch(ctx context.Context, id uint) (*Branch, error)
	UpdateBranch(ctx context.Context, id uint, in Branch) (*Branch, error)
	ListBranchStaff(ctx context.Context, id uint) ([]Staff, error)
	ListStaff(ctx context.Context) ([]Staff, error)
	CreateStaff(ctx context.Context, in Staff) (*Staff, error)
	GetStaff(ctx context.Context, id uint) (*Staff, error)
	UpdateStaff(ctx context.Context, id uint, in Staff) (*Staff, error)
	ListRoles(ctx context.Context) ([]Role, error)
	ListPermissions(ctx context.Context) ([]Permission, error)
	ListRolePermissions(ctx context.Context, id uint) ([]Permission, error)
	CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) Login(ctx context.Context) (*Session, error) {
	return s.repo.Login(ctx)
}

func (s *Service) RefreshSession(ctx context.Context) (*Session, error) {
	return s.repo.RefreshSession(ctx)
}

func (s *Service) Logout(ctx context.Context) error {
	return s.repo.Logout(ctx)
}

func (s *Service) GetMe(ctx context.Context) (*User, error) {
	return s.repo.GetMe(ctx)
}

func (s *Service) UpdateMe(ctx context.Context, in User) (*User, error) {
	return s.repo.UpdateMe(ctx, in)
}

func (s *Service) VerifyManagerPIN(ctx context.Context) (*Staff, error) {
	return s.repo.VerifyManagerPIN(ctx)
}

func (s *Service) VerifyStaffPIN(ctx context.Context, id uint) (*Staff, error) {
	return s.repo.VerifyStaffPIN(ctx, id)
}

func (s *Service) RegisterDevice(ctx context.Context, in Device) (*Device, error) {
	return s.repo.RegisterDevice(ctx, in)
}

func (s *Service) ListDevices(ctx context.Context) ([]Device, error) {
	return s.repo.ListDevices(ctx)
}

func (s *Service) UpdateDevice(ctx context.Context, id uint, in Device) (*Device, error) {
	return s.repo.UpdateDevice(ctx, id, in)
}

func (s *Service) ListBranches(ctx context.Context) ([]Branch, error) {
	return s.repo.ListBranches(ctx)
}

func (s *Service) CreateBranch(ctx context.Context, in Branch) (*Branch, error) {
	return s.repo.CreateBranch(ctx, in)
}

func (s *Service) GetBranch(ctx context.Context, id uint) (*Branch, error) {
	return s.repo.GetBranch(ctx, id)
}

func (s *Service) UpdateBranch(ctx context.Context, id uint, in Branch) (*Branch, error) {
	return s.repo.UpdateBranch(ctx, id, in)
}

func (s *Service) ListBranchStaff(ctx context.Context, id uint) ([]Staff, error) {
	return s.repo.ListBranchStaff(ctx, id)
}

func (s *Service) ListStaff(ctx context.Context) ([]Staff, error) {
	return s.repo.ListStaff(ctx)
}

func (s *Service) CreateStaff(ctx context.Context, in Staff) (*Staff, error) {
	return s.repo.CreateStaff(ctx, in)
}

func (s *Service) GetStaff(ctx context.Context, id uint) (*Staff, error) {
	return s.repo.GetStaff(ctx, id)
}

func (s *Service) UpdateStaff(ctx context.Context, id uint, in Staff) (*Staff, error) {
	return s.repo.UpdateStaff(ctx, id, in)
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.repo.ListRoles(ctx)
}

func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	return s.repo.ListPermissions(ctx)
}

func (s *Service) ListRolePermissions(ctx context.Context, id uint) ([]Permission, error) {
	return s.repo.ListRolePermissions(ctx, id)
}

func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error) {
	return s.repo.CreateAccount(ctx, in)
}
