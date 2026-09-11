package returns

// Interface defines the returns domain's use cases. TODO: define methods as endpoints are implemented.
type Interface interface {
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)
