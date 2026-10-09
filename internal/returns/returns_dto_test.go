package returns

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

// allVoidReasons is every VoidReason constant. The DTO's oneof tag is a string
// literal, so this guards against a constant being added without the tag
// being updated (the request would then 400 for a code the model accepts).
var allVoidReasons = []VoidReason{
	VoidReasonCustomerRequest, VoidReasonPriceError, VoidReasonItemError, VoidReasonStaffError, VoidReasonOther,
	VoidReasonDuplicateTransaction, VoidReasonWrongOrder, VoidReasonIncorrectPayment, VoidReasonCashierMistake,
}

func TestVoidSaleRequest_EveryVoidReasonIsAccepted(t *testing.T) {
	for _, reason := range allVoidReasons {
		require.NoError(t, binding.Validator.ValidateStruct(VoidSaleRequest{Reason: reason}), string(reason))
	}
}

func TestVoidSaleRequest_TillReasonCodes(t *testing.T) {
	for _, code := range []string{"duplicate_transaction", "wrong_order", "incorrect_payment", "cashier_mistake"} {
		require.NoError(t, binding.Validator.ValidateStruct(VoidSaleRequest{Reason: VoidReason(code)}), code)
	}
}

// The till's void screen has no explanation box, so it sends none.
func TestVoidSaleRequest_EmptyExplanationIsAccepted(t *testing.T) {
	require.NoError(t, binding.Validator.ValidateStruct(VoidSaleRequest{Reason: VoidReasonWrongOrder, Explanation: ""}))
}

func TestVoidSaleRequest_UnknownReasonIsRejected(t *testing.T) {
	require.Error(t, binding.Validator.ValidateStruct(VoidSaleRequest{Reason: "because_i_said_so", Explanation: "x"}))
}

func TestVoidSaleRequest_MissingReasonIsRejected(t *testing.T) {
	require.Error(t, binding.Validator.ValidateStruct(VoidSaleRequest{Explanation: "x"}))
}

func TestCreateReturnItemRequest_EveryItemConditionIsAccepted(t *testing.T) {
	for _, condition := range []ItemCondition{
		ItemConditionSellable, ItemConditionDamaged, ItemConditionOpened, ItemConditionDefective,
		ItemConditionExpired, ItemConditionOther,
	} {
		require.NoError(t, binding.Validator.ValidateStruct(
			CreateReturnItemRequest{SaleItemID: 1, Qty: 1, Condition: condition}), string(condition))
	}
}

func TestCreateReturnItemRequest_UnknownConditionIsRejected(t *testing.T) {
	require.Error(t, binding.Validator.ValidateStruct(
		CreateReturnItemRequest{SaleItemID: 1, Qty: 1, Condition: "melted"}))
}

func TestCreateExchangeItemRequest_ConditionIsOptional(t *testing.T) {
	require.NoError(t, binding.Validator.ValidateStruct(
		CreateExchangeItemRequest{Direction: DirectionIn, SaleItemID: new(uint), Qty: 1}))
}

func TestCreateExchangeItemRequest_EveryItemConditionIsAccepted(t *testing.T) {
	for _, condition := range []ItemCondition{
		ItemConditionSellable, ItemConditionDamaged, ItemConditionOpened, ItemConditionDefective,
		ItemConditionExpired, ItemConditionOther,
	} {
		require.NoError(t, binding.Validator.ValidateStruct(
			CreateExchangeItemRequest{Direction: DirectionIn, SaleItemID: new(uint), Qty: 1, Condition: condition}), string(condition))
	}
}

func TestCreateExchangeItemRequest_UnknownConditionIsRejected(t *testing.T) {
	require.Error(t, binding.Validator.ValidateStruct(
		CreateExchangeItemRequest{Direction: DirectionIn, SaleItemID: new(uint), Qty: 1, Condition: "melted"}))
}
