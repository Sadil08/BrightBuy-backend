package httpapi

import "brightbuy-backend/internal/identity/domain"

func toUserProfileDTO(a domain.Account) UserProfileDTO {
	return UserProfileDTO{
		UserID:     a.ID,
		CustomerID: a.CustomerID,
		Name:       a.Name,
		Email:      a.Email,
		Role:       a.RoleName,
	}
}

func toUserProfileDTOs(accounts []domain.Account) []UserProfileDTO {
	dtos := make([]UserProfileDTO, 0, len(accounts))
	for _, a := range accounts {
		dtos = append(dtos, toUserProfileDTO(a))
	}
	return dtos
}

// toRoleDTO: PermissionCodes defaults to `[]string{}`, not nil, for the identical reason catalog's
// toVariantAttributeDTO does — a role with zero granted permissions (freshly stripped via
// SetRolePermissions, or just ADMIN-only semantics) must serialize as `"permissions":[]`, not
// `"permissions":null`; openapi.yaml declares it a plain array.
func toRoleDTO(r domain.RoleWithPermissions) RoleDTO {
	codes := r.PermissionCodes
	if codes == nil {
		codes = []string{}
	}
	return RoleDTO{RoleID: r.ID, Name: r.Name, Permissions: codes}
}

func toRoleDTOs(roles []domain.RoleWithPermissions) []RoleDTO {
	dtos := make([]RoleDTO, 0, len(roles))
	for _, r := range roles {
		dtos = append(dtos, toRoleDTO(r))
	}
	return dtos
}

func toPermissionDTO(p domain.Permission) PermissionDTO {
	return PermissionDTO{PermissionID: p.ID, Code: p.Code, Description: p.Description}
}

func toPermissionDTOs(perms []domain.Permission) []PermissionDTO {
	dtos := make([]PermissionDTO, 0, len(perms))
	for _, p := range perms {
		dtos = append(dtos, toPermissionDTO(p))
	}
	return dtos
}
