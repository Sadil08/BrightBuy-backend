package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"brightbuy-backend/internal/ordering/domain"
)

var ErrInvalidTransition = errors.New("invalid order status transition")

type InvalidTransitionError struct {
	From string
	To   string
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("cannot transition from %s to %s", e.From, e.To)
}
func (e *InvalidTransitionError) Unwrap() error { return ErrInvalidTransition }

var validTransitions = map[string][]string{
	"Placed":         {"Confirmed", "Cancelled"},
	"Confirmed":      {"Processing", "Cancelled"},
	"Processing":     {"Shipped", "ReadyForPickup", "Cancelled"},
	"Shipped":        {"Delivered", "Cancelled"},
	"ReadyForPickup": {"Delivered", "Cancelled"},
	"Delivered":      {"Completed"},
}

type OrderStatusRepository interface {
	GetByIDForStaff(context.Context, int) (*domain.Order, error)
	SetActingUser(context.Context, *sql.Tx, int) error
	SetStatus(context.Context, *sql.Tx, int, string) error
	CallCancelOrder(context.Context, int, int) error
}

type PaymentStatusRepository interface {
	SetStatus(context.Context, *sql.Tx, int, string) error
}

type OrderStatusService struct {
	repo     OrderStatusRepository
	payments PaymentStatusRepository
	withTx   TransactionRunner
}

func NewOrderStatusService(repo OrderStatusRepository, payments PaymentStatusRepository, withTx TransactionRunner) *OrderStatusService {
	return &OrderStatusService{repo: repo, payments: payments, withTx: withTx}
}

func (s *OrderStatusService) UpdateStatus(ctx context.Context, actingUserID, orderID int, newStatus string) error {
	if newStatus == "Cancelled" {
		return s.Cancel(ctx, actingUserID, orderID)
	}
	order, err := s.repo.GetByIDForStaff(ctx, orderID)
	if err != nil {
		return err
	}
	if !slices.Contains(validTransitions[string(order.Status)], newStatus) {
		return &InvalidTransitionError{From: string(order.Status), To: newStatus}
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.repo.SetActingUser(ctx, tx, actingUserID); err != nil {
			return err
		}
		if err := s.repo.SetStatus(ctx, tx, orderID, newStatus); err != nil {
			return err
		}
		if newStatus == "Delivered" && order.PaymentMethod == "COD" {
			if err := s.payments.SetStatus(ctx, tx, orderID, "Paid"); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *OrderStatusService) Cancel(ctx context.Context, actingUserID, orderID int) error {
	return s.repo.CallCancelOrder(ctx, orderID, actingUserID)
}
