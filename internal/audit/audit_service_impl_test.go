package audit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_ListAuditLog_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

	branchID := uint(5)
	want := []AuditLog{{ID: 1, OrgID: 7, Entity: "sale", EntityID: "abc", Action: "voided"}}
	repo.EXPECT().ListAuditLog(mock.Anything, uint(7), &branchID).Return(want, nil).Once()

	got, err := svc.ListAuditLog(context.Background(), 7, &branchID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListAuditLog_OrgWideWhenNoBranchGiven(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

	want := []AuditLog{{ID: 1, OrgID: 7}, {ID: 2, OrgID: 7}}
	repo.EXPECT().ListAuditLog(mock.Anything, uint(7), (*uint)(nil)).Return(want, nil).Once()

	got, err := svc.ListAuditLog(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestToJSON_NilMarshalsToJSONNull(t *testing.T) {
	got := ToJSON(nil)
	require.Equal(t, "null", string(got))
}

func TestToJSON_MarshalsStruct(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
	}
	got := ToJSON(sample{Name: "test"})
	require.JSONEq(t, `{"name":"test"}`, string(got))
}
