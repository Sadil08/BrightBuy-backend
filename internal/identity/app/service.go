package app

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailExists        = errors.New("email already registered")
	ErrInvalidInput       = errors.New("invalid registration input")
)

type Account struct {
	UserID       int
	CustomerID   int
	Email        string
	PasswordHash string
	Active       bool
}

type Repository interface {
	CreateCustomer(context.Context, string, string, string) (Account, error)
	FindByEmail(context.Context, string) (Account, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Register(ctx context.Context, name, email, password string) (Account, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	if name == "" || len(name) > 100 || len(email) > 255 || len(password) < 8 || len(password) > 72 {
		return Account{}, ErrInvalidInput
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return Account{}, ErrInvalidInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return Account{}, fmt.Errorf("hash password: %w", err)
	}
	account, err := s.repo.CreateCustomer(ctx, name, email, string(hash))
	if err != nil {
		if errors.Is(err, ErrEmailExists) {
			return Account{}, ErrEmailExists
		}
		return Account{}, fmt.Errorf("create customer account: %w", err)
	}
	return account, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (Account, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	account, err := s.repo.FindByEmail(ctx, email)
	if err != nil || !account.Active || bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		return Account{}, ErrInvalidCredentials
	}
	account.PasswordHash = ""
	return account, nil
}
