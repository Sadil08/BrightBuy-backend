package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/identity/app"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"
)

// AdminHandler is the admin-only half (FR-AUTH-9, FR-AUTH-10) — every method here sits behind
// RequirePermission in RegisterRoutes, never checked again inside the handler itself. A handler
// trusting its own routing to have already gated the request is the same pattern catalog's handlers
// trust their DTOs were already validated before they run.
type AdminHandler struct {
	accounts *app.AccountService
}

func NewAdminHandler(accounts *app.AccountService) *AdminHandler {
	return &AdminHandler{accounts: accounts}
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page")) // 0 on a parse failure — Normalize* below treats
	size, _ := strconv.Atoi(query.Get("size")) // that identically to "not provided at all"
	page, size = app.NormalizePage(page), app.NormalizePageSize(size)

	accounts, total, err := h.accounts.ListAccounts(r.Context(), page, size)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, UserListDTO{
		Items: toUserProfileDTOs(accounts),
		Page:  PageDTO{Page: page, Size: size, Total: total},
	})
}

// CreateAccount is FR-AUTH-9 — the ONLY way a WAREHOUSE_STAFF/ORDER_MANAGER/MANAGER/ADMIN account
// ever gets created; there is no self-registration path for any of these roles anywhere in this
// codebase (AuthService.Register always and only creates CUSTOMER).
func (h *AdminHandler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	req, err := httpx.DecodeAndValidate[CreateAccountRequestDTO](r)
	if err != nil {
		httpx.WriteValidationError(w, err)
		return
	}

	acct, err := h.accounts.CreateStaffAccount(r.Context(), req.Name, req.Email, req.Password, req.RoleID)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrUnknownRole):
			httpx.WriteError(w, http.StatusBadRequest, "UNKNOWN_ROLE", "no such role") // plan.md §6: 400, not 500
		case errors.Is(err, app.ErrEmailAlreadyRegistered):
			httpx.WriteError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "an account with this email already exists")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toUserProfileDTO(*acct))
}

func (h *AdminHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.accounts.ListRoles(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRoleDTOs(roles))
}

func (h *AdminHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := h.accounts.ListPermissions(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPermissionDTOs(perms))
}

// SetRolePermissions is FR-AUTH-10 / AC-AUTH-7 — works identically for every role, including ADMIN's
// own (accepted, per plan.md's "for console display consistency"); there's no special case anywhere
// in this handler or the service beneath it, because real enforcement never reads this data for that
// role in the first place (identity/app's resolvePermissions).
func (h *AdminHandler) SetRolePermissions(w http.ResponseWriter, r *http.Request) {
	roleID, err := strconv.Atoi(chi.URLParam(r, "roleId"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_ID", "role id must be a number")
		return
	}

	req, err := httpx.DecodeAndValidate[RolePermissionsUpdateRequestDTO](r)
	if err != nil {
		httpx.WriteValidationError(w, err)
		return
	}

	// grantedByUserID is for the audit trail (role_permission.granted_by_user_id,
	// specs/global/02_SECURITY_BASELINE.md §7) — always present here, since this route sits behind
	// auth.Authenticate in RegisterRoutes.
	grantedByUserID := 0
	if claims := auth.ClaimsFromContext(r.Context()); claims != nil {
		grantedByUserID = claims.UserID
	}

	result, err := h.accounts.SetRolePermissions(r.Context(), roleID, req.PermissionCodes, grantedByUserID)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrUnknownRole):
			httpx.WriteError(w, http.StatusBadRequest, "UNKNOWN_ROLE", "no such role")
		case errors.Is(err, app.ErrUnknownPermissionCode):
			httpx.WriteError(w, http.StatusBadRequest, "UNKNOWN_PERMISSION_CODE", "an unknown permission code was supplied")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toRoleDTO(*result))
}
