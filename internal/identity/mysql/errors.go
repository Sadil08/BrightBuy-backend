package mysql

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// isDuplicateKeyError reports whether err is MySQL's own "duplicate entry" error (code 1062) — what
// a unique-index violation (user_account's uq_user_account_email, FR-AUTH-2) actually looks like
// coming back from the driver. Checking the driver's own typed error rather than string-matching
// err.Error() means this keeps working even if MySQL ever reworded the message text.
func isDuplicateKeyError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
