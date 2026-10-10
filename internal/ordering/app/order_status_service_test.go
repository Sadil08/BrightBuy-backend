package app

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"brightbuy-backend/internal/ordering/domain"
)

func TestValidTransitionsMapCompleteness(t *testing.T) {
	allStatuses := []string{
		"Placed", "Confirmed", "Processing", "Shipped", "ReadyForPickup", "Delivered", "Completed", "Cancelled",
	}
	terminalStatuses := map[string]bool{
		"Completed": true,
		"Cancelled": true,
	}

	for _, status := range allStatuses {
		outgoing, ok := validTransitions[status]
		isTerminal := terminalStatuses[status]

		if isTerminal {
			if ok && len(outgoing) > 0 {
				t.Errorf("expected terminal status %s to have no outgoing edges, got %v", status, outgoing)
			}
		} else {
			if !ok || len(outgoing) == 0 {
				t.Errorf("expected non-terminal status %s to have at least one outgoing edge", status)
			}
		}
	}
}

type mockOrderStatusRepo struct {
	order *domain.Order
	err   error
	set   bool
}

func (m *mockOrderStatusRepo) GetByIDForStaff(ctx context.Context, id int) (*domain.Order, error) {
	return m.order, m.err
}
func (m *mockOrderStatusRepo) SetActingUser(ctx context.Context, tx *sql.Tx, acting int) error {
	return nil
}
func (m *mockOrderStatusRepo) SetStatus(ctx context.Context, tx *sql.Tx, id int, status string) error {
	m.set = true
	return nil
}
func (m *mockOrderStatusRepo) CallCancelOrder(ctx context.Context, id int, acting int) error {
	return nil
}

type mockPaymentRepo struct {
	set bool
}

func (m *mockPaymentRepo) SetStatus(ctx context.Context, tx *sql.Tx, id int, status string) error {
	m.set = true
	return nil
}

func mockTxRunner(ctx context.Context, fn func(*sql.Tx) error) error {
	return fn(nil)
}

func TestUpdateStatus_CODToPaid(t *testing.T) {
	repo := &mockOrderStatusRepo{
		order: &domain.Order{Status: "Shipped", PaymentMethod: domain.PaymentCOD},
	}
	payments := &mockPaymentRepo{}
	svc := NewOrderStatusService(repo, payments, mockTxRunner)

	err := svc.UpdateStatus(context.Background(), 1, 1, "Delivered")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.set {
		t.Error("expected order status to be set")
	}
	if !payments.set {
		t.Error("expected payment status to be set to Paid for COD")
	}
}

func TestUpdateStatus_CardNoPaidSideEffect(t *testing.T) {
	repo := &mockOrderStatusRepo{
		order: &domain.Order{Status: "Shipped", PaymentMethod: domain.PaymentCard},
	}
	payments := &mockPaymentRepo{}
	svc := NewOrderStatusService(repo, payments, mockTxRunner)

	err := svc.UpdateStatus(context.Background(), 1, 1, "Delivered")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.set {
		t.Error("expected order status to be set")
	}
	if payments.set {
		t.Error("expected payment status NOT to be set for Card")
	}
}

func TestUpdateStatus_InvalidTransition(t *testing.T) {
	repo := &mockOrderStatusRepo{
		order: &domain.Order{Status: "Delivered"},
	}
	svc := NewOrderStatusService(repo, nil, mockTxRunner)

	err := svc.UpdateStatus(context.Background(), 1, 1, "Processing")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}
