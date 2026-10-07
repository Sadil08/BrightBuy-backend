package app

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type fakeRepository struct {
	account Account
	email   string
	name    string
	err     error
}

func (f *fakeRepository) CreateCustomer(_ context.Context, name, email, hash string) (Account, error) {
	f.name, f.email = name, email
	if f.err != nil {
		return Account{}, f.err
	}
	f.account = Account{UserID: 1, CustomerID: 2, Email: email, PasswordHash: hash, Active: true}
	return f.account, nil
}
func (f *fakeRepository) FindByEmail(_ context.Context, email string) (Account, error) {
	f.email = email
	if f.err != nil {
		return Account{}, f.err
	}
	if email != f.account.Email {
		return Account{}, ErrInvalidCredentials
	}
	return f.account, nil
}

func TestRegisterHashesPasswordAndNormalizesEmail(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)
	account, err := service.Register(context.Background(), "  Jane Doe ", " JANE@example.com ", "password-123")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if repo.name != "Jane Doe" || repo.email != "jane@example.com" {
		t.Fatalf("stored name/email = %q/%q", repo.name, repo.email)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte("password-123")); err != nil {
		t.Fatalf("stored password is not a valid bcrypt hash: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(account.PasswordHash))
	if err != nil || cost < 12 {
		t.Fatalf("bcrypt cost = %d, err = %v; want >= 12", cost, err)
	}
}

func TestLoginReturnsGenericErrorForMissingOrWrongPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password-123"), 12)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepository{account: Account{UserID: 1, CustomerID: 2, Email: "jane@example.com", PasswordHash: string(hash), Active: true}}
	service := NewService(repo)
	if _, err := service.Login(context.Background(), "missing@example.com", "password-123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("missing account error = %v, want ErrInvalidCredentials", err)
	}
	repo.err = nil
	repo.account.Email = "jane@example.com"
	if _, err := service.Login(context.Background(), "jane@example.com", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v, want ErrInvalidCredentials", err)
	}
}
