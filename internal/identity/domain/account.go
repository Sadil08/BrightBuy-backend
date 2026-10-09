package domain

// Account is one user_account row plus the profile fields every caller of this module ends up
// needing alongside it (its role's name, and whichever profile name applies). Denormalized
// deliberately: login, /auth/me, and registration's response all need "the account plus its display
// name plus its role name" as one unit, so the repository joins all of this in a single query rather
// than making every caller do a second round trip for a name it almost always wants anyway.
type Account struct {
	ID           int
	Email        string
	PasswordHash string
	IsActive     bool
	RoleID       int
	RoleName     string
	Name         string // from customer.name or staff_profile.name, whichever this account has
	// CustomerID is nil for every non-CUSTOMER role — a staff/manager/admin account has a
	// staff_profile row, never a customer row. A plain int can't represent "doesn't apply here,"
	// same reasoning as catalog's ListFilter.CategoryID.
	CustomerID *int
}
