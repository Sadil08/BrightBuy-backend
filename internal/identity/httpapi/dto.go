package httpapi

// Request DTOs — the `validate` tags are what httpx.DecodeAndValidate checks BEFORE any of these
// ever reaches a service (specs/global/02_SECURITY_BASELINE.md §4). Field-for-field against
// specs/openapi/openapi.yaml's schemas of the same name.

type RegisterRequestDTO struct {
	Name     string `json:"name" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"` // FR-AUTH-7
	Phone    string `json:"phone" validate:"required"`
}

type LoginRequestDTO struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type CreateAccountRequestDTO struct {
	Name     string `json:"name" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
	RoleID   int    `json:"roleId" validate:"required"`
}

type RolePermissionsUpdateRequestDTO struct {
	// No `required` on the slice itself: an EMPTY list ([]) is a legitimate request — "revoke
	// everything from this role" — validator's `required` on a slice would reject that as if it
	// were missing entirely, which is the opposite of what AC-AUTH-7-adjacent admin actions need.
	PermissionCodes []string `json:"permissionCodes"`
}

// Response DTOs.

type UserProfileDTO struct {
	UserID     int    `json:"userId"`
	CustomerID *int   `json:"customerId,omitempty"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
}

type PageDTO struct {
	Page  int `json:"page"`
	Size  int `json:"size"`
	Total int `json:"total"`
}

type UserListDTO struct {
	Items []UserProfileDTO `json:"items"`
	Page  PageDTO          `json:"page"`
}

type RoleDTO struct {
	RoleID      int      `json:"roleId"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

type PermissionDTO struct {
	PermissionID int    `json:"permissionId"`
	Code         string `json:"code"`
	Description  string `json:"description"`
}
