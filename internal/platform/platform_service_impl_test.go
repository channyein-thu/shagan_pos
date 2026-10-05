package platform

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against mocked dependencies. Same reasoning as
// catalog's equivalent; *gorm.DB satisfies common.Transactioner natively in
// production.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

// testQRImageBytes returns real, valid PNG bytes - image.DecodeConfig (used
// by Service.UploadPaymentQRCode) needs to actually decode a real image
// header, which isn't something a mock can stand in for.
func testQRImageBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return buf.Bytes()
}

func TestService_GetReceiptSettings_BranchOverrideExists_ReturnsIt(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branchID := uint(5)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), branchID).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	want := &ReceiptSetting{ID: 2, OrgID: 7, BranchID: &branchID, ShopName: "Branch Shop"}
	repo.EXPECT().GetReceiptSettings(mock.Anything, uint(7), &branchID).Return(want, nil).Once()

	got, err := svc.GetReceiptSettings(context.Background(), 7, &branchID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_GetReceiptSettings_NoBranchOverride_FallsBackToOrgDefault(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branchID := uint(5)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), branchID).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetReceiptSettings(mock.Anything, uint(7), &branchID).Return(nil, common.NotFoundError("receipt settings not found")).Once()
	want := &ReceiptSetting{ID: 1, OrgID: 7, ShopName: "Org Default"}
	repo.EXPECT().GetReceiptSettings(mock.Anything, uint(7), (*uint)(nil)).Return(want, nil).Once()

	got, err := svc.GetReceiptSettings(context.Background(), 7, &branchID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_GetReceiptSettings_NilBranchID_ReturnsOrgDefaultDirectly(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	want := &ReceiptSetting{ID: 1, OrgID: 7, ShopName: "Org Default"}
	repo.EXPECT().GetReceiptSettings(mock.Anything, uint(7), (*uint)(nil)).Return(want, nil).Once()

	got, err := svc.GetReceiptSettings(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_GetReceiptSettings_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branchID := uint(5)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), branchID).Return(nil, common.NotFoundError("branch not found")).Once()

	_, err := svc.GetReceiptSettings(context.Background(), 7, &branchID)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateReceiptSettings_OrgDefault_Upserts(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	in := UpdateReceiptSettingsRequest{ShopName: "Shop", Address: "Addr", Phone: "123", ThankYou: "Thanks"}
	want := &ReceiptSetting{ID: 1, OrgID: 7, ShopName: "Shop"}
	repo.EXPECT().UpsertReceiptSettings(mock.Anything, uint(7), in).Return(want, nil).Once()

	got, err := svc.UpdateReceiptSettings(context.Background(), 7, in)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_UpdateReceiptSettings_BranchOverride_ValidatesOwnershipThenUpserts(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branchID := uint(5)
	in := UpdateReceiptSettingsRequest{BranchID: &branchID, ShopName: "Shop", Address: "Addr", Phone: "123", ThankYou: "Thanks"}
	branches.EXPECT().GetBranch(mock.Anything, uint(7), branchID).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	want := &ReceiptSetting{ID: 2, OrgID: 7, BranchID: &branchID, ShopName: "Shop"}
	repo.EXPECT().UpsertReceiptSettings(mock.Anything, uint(7), in).Return(want, nil).Once()

	got, err := svc.UpdateReceiptSettings(context.Background(), 7, in)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_UpdateReceiptSettings_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branchID := uint(5)
	in := UpdateReceiptSettingsRequest{BranchID: &branchID, ShopName: "Shop", Address: "Addr", Phone: "123", ThankYou: "Thanks"}
	branches.EXPECT().GetBranch(mock.Anything, uint(7), branchID).Return(nil, common.NotFoundError("branch not found")).Once()

	_, err := svc.UpdateReceiptSettings(context.Background(), 7, in)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_TestPrinter_HappyPath_ReturnsCannedPayload(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7, Name: "Main St"}, nil).Once()

	got, err := svc.TestPrinter(context.Background(), 7, 5)
	require.NoError(t, err)
	require.Equal(t, "ok", got["status"])
	require.Equal(t, uint(5), got["branch_id"])
	lines, ok := got["lines"].([]string)
	require.True(t, ok)
	require.Contains(t, lines, "Main St")
}

func TestService_TestPrinter_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(nil, common.NotFoundError("branch not found")).Once()

	_, err := svc.TestPrinter(context.Background(), 7, 5)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UploadPaymentQRCode_HappyPath_CreatesAndUploads(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	imgBytes := testQRImageBytes(t)
	file := bytes.NewReader(imgBytes)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListActivePaymentQRCodes(mock.Anything, uint(5)).Return(nil, nil).Once()
	repo.EXPECT().
		CreatePaymentQRCode(mock.Anything, mock.MatchedBy(func(qr *PaymentQRCode) bool {
			return qr.BranchID == 5 && qr.BankName == "KBZ Bank" && qr.IsActive
		})).
		Run(func(_ *gorm.DB, qr *PaymentQRCode) { qr.ID = 1 }).
		Return(nil).
		Once()
	store.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, int64(len(imgBytes)), "image/png").Return(nil).Once()
	store.EXPECT().PresignedURL(mock.Anything, mock.Anything, DefaultQRImageURLTTL).Return("https://signed.example/qr.png", nil).Once()

	got, err := svc.UploadPaymentQRCode(context.Background(), 7, 5, "KBZ Bank", file, int64(len(imgBytes)), "image/png")
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
	require.Equal(t, "KBZ Bank", got.BankName)
	require.Equal(t, "https://signed.example/qr.png", got.ImageURL)
}

func TestService_UploadPaymentQRCode_CapReached_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	active := make([]PaymentQRCode, MaxActivePaymentQRCodesPerBranch)
	for i := range active {
		active[i] = PaymentQRCode{ID: uint(i + 1), BranchID: 5, BankName: "Bank", IsActive: true}
	}
	repo.EXPECT().ListActivePaymentQRCodes(mock.Anything, uint(5)).Return(active, nil).Once()
	// CreatePaymentQRCode must never be called - cap already reached.

	imgBytes := testQRImageBytes(t)
	_, err := svc.UploadPaymentQRCode(context.Background(), 7, 5, "New Bank", bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png")
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UploadPaymentQRCode_DuplicateBankName_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListActivePaymentQRCodes(mock.Anything, uint(5)).
		Return([]PaymentQRCode{{ID: 1, BranchID: 5, BankName: "KBZ Bank", IsActive: true}}, nil).Once()
	// CreatePaymentQRCode must never be called - duplicate active bank name.

	imgBytes := testQRImageBytes(t)
	_, err := svc.UploadPaymentQRCode(context.Background(), 7, 5, "KBZ Bank", bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png")
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_UploadPaymentQRCode_InvalidImage_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListActivePaymentQRCodes(mock.Anything, uint(5)).Return(nil, nil).Once()
	// CreatePaymentQRCode must never be called - not a decodable image.

	notAnImage := bytes.NewReader([]byte("this is not an image"))
	_, err := svc.UploadPaymentQRCode(context.Background(), 7, 5, "KBZ Bank", notAnImage, int64(notAnImage.Len()), "image/png")
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UploadPaymentQRCode_EmptyBankName_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	// ListActivePaymentQRCodes must never be called - fails validation first.

	imgBytes := testQRImageBytes(t)
	_, err := svc.UploadPaymentQRCode(context.Background(), 7, 5, "", bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png")
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UploadPaymentQRCode_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(nil, common.NotFoundError("branch not found")).Once()

	imgBytes := testQRImageBytes(t)
	_, err := svc.UploadPaymentQRCode(context.Background(), 7, 5, "KBZ Bank", bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png")
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListPaymentQRCodes_HappyPath_ReturnsSignedURLs(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListActivePaymentQRCodes(mock.Anything, uint(5)).
		Return([]PaymentQRCode{{ID: 1, BranchID: 5, BankName: "KBZ Bank", StorageKey: "key-1", IsActive: true}}, nil).Once()
	store.EXPECT().PresignedURL(mock.Anything, "key-1", DefaultQRImageURLTTL).Return("https://signed.example/1.png", nil).Once()

	got, err := svc.ListPaymentQRCodes(context.Background(), 7, 5)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "https://signed.example/1.png", got[0].ImageURL)
}

func TestService_ListPaymentQRCodes_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(nil, common.NotFoundError("branch not found")).Once()

	_, err := svc.ListPaymentQRCodes(context.Background(), 7, 5)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_DeletePaymentQRCode_HappyPath_DeactivatesAndDeletesImage(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetPaymentQRCode(mock.Anything, uint(5), uint(1)).
		Return(&PaymentQRCode{ID: 1, BranchID: 5, StorageKey: "key-1", IsActive: true}, nil).Once()
	repo.EXPECT().DeactivatePaymentQRCode(mock.Anything, uint(1)).Return(nil).Once()
	store.EXPECT().Delete(mock.Anything, "key-1").Return(nil).Once()

	err := svc.DeletePaymentQRCode(context.Background(), 7, 5, 1)
	require.NoError(t, err)
}

func TestService_DeletePaymentQRCode_AlreadyInactive_ReturnsNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetPaymentQRCode(mock.Anything, uint(5), uint(1)).
		Return(&PaymentQRCode{ID: 1, BranchID: 5, StorageKey: "key-1", IsActive: false}, nil).Once()
	// DeactivatePaymentQRCode must never be called - already inactive.

	err := svc.DeletePaymentQRCode(context.Background(), 7, 5, 1)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_DeletePaymentQRCode_NotFound_Propagates(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetPaymentQRCode(mock.Anything, uint(5), uint(1)).
		Return(nil, common.NotFoundError("payment QR code not found")).Once()

	err := svc.DeletePaymentQRCode(context.Background(), 7, 5, 1)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_DeletePaymentQRCode_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, fakeTransactioner{}, NewMockStorage(t))

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(nil, common.NotFoundError("branch not found")).Once()

	err := svc.DeletePaymentQRCode(context.Background(), 7, 5, 1)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}
