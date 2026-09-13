package common

import "errors"

// ErrNotImplemented is returned by stubbed repository/service methods until
// their real logic is written.
var ErrNotImplemented = errors.New("not implemented")
