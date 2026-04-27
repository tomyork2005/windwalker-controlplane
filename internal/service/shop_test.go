package service

import (
	"context"
	"testing"
	"time"

	"control-plane/internal/model"
	servicemocks "control-plane/internal/service/mocks"

	"github.com/gojuno/minimock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShopService_CreateInvoice_propagatesProviderOrderID(t *testing.T) {
	t.Parallel()

	const (
		telegramID      = int64(7)
		username        = "alice"
		chatID          = int64(7)
		userID          = "u-1"
		planID          = "p-1"
		providerOrderID = "tx-99"
	)

	ctrl := minimock.NewController(t)
	payMock := servicemocks.NewShopPaymentMock(ctrl)
	storageMock := servicemocks.NewShopStorageMock(ctrl)
	outboxMock := servicemocks.NewShopOutboxMock(ctrl)

	plan := &model.Plan{
		ID:           planID,
		Region:       "ru",
		DriverType:   "vless",
		Money:        model.Money{Amount: 200, Curr: model.RUBCurrency},
		DurationDays: 30,
	}

	storageMock.UpsertUserByTelegramIDMock.
		Expect(minimock.AnyContext, telegramID, username).
		Return(&model.User{ID: userID}, nil)
	storageMock.GetPlanByIDMock.Expect(minimock.AnyContext, planID).Return(plan, nil)

	payMock.CreatePaymentOrderMock.Set(func(_ context.Context, _ model.CreateOrderInput) (model.CreateOrderOutput, error) {
		return model.CreateOrderOutput{
			ProviderName:    "Platega",
			ProviderOrderID: providerOrderID,
			RedirectURL:     "https://pay/x",
			ExpiredAt:       time.Now().Add(15 * time.Minute),
		}, nil
	})

	storageMock.CreateInvoiceMock.Inspect(func(_ context.Context, inv *model.Invoice) {
		assert.Equal(t, providerOrderID, inv.ProviderOrderID)
		assert.Equal(t, "Platega", inv.PaymentProvider)
		assert.Equal(t, model.CreatedInvoiceStatus, inv.Status)
	}).Return(nil)

	svc := NewShopService(payMock, storageMock, outboxMock)
	inv, err := svc.CreateInvoice(t.Context(), planID, telegramID, username, "sbp", chatID)
	require.NoError(t, err)
	assert.Equal(t, providerOrderID, inv.ProviderOrderID)
}
