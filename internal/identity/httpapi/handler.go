package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"brightbuy-backend/internal/identity/app"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"
)

type Handler struct {
	service      *app.Service
	signingKey   []byte
	cookieSecure bool
}

func NewHandler(service *app.Service, signingKey []byte, cookieSecure bool) *Handler {
	return &Handler{service: service, signingKey: signingKey, cookieSecure: cookieSecure}
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionResponse struct {
	UserID     int    `json:"userId"`
	CustomerID int    `json:"customerId"`
	Email      string `json:"email"`
	Role       string `json:"role"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		return
	}
	account, err := h.service.Register(r.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "name, email, or password is invalid")
		case errors.Is(err, app.ErrEmailExists):
			httpx.WriteError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "email is already registered")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "account registration failed")
		}
		return
	}
	h.writeSession(w, http.StatusCreated, account)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		return
	}
	account, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, app.ErrInvalidCredentials) {
			httpx.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is incorrect")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "login failed")
		return
	}
	h.writeSession(w, http.StatusOK, account)
}

func (h *Handler) writeSession(w http.ResponseWriter, status int, account app.Account) {
	token, err := auth.IssueCustomerToken(h.signingKey, account.UserID, account.CustomerID, time.Now())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not create session")
		return
	}
	auth.SetCustomerCookie(w, token, h.cookieSecure)
	httpx.WriteJSON(w, status, sessionResponse{
		UserID: account.UserID, CustomerID: account.CustomerID, Email: account.Email, Role: "CUSTOMER",
	})
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func RegisterRoutes(r interface {
	Post(string, http.HandlerFunc)
}, h *Handler) {
	r.Post("/auth/register", h.Register)
	r.Post("/auth/login", h.Login)
}
