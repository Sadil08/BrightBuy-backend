package app

import (
	"context"
	"errors"
	"testing"

	"brightbuy-backend/internal/inventory/domain"
)

// ---------------------------------------------------------------
// FAKE REPOSITORY
// ---------------------------------------------------------------
// The real repository talks to the database. In unit tests we don't
// want that (it is slow and needs setup). So we make a "fake" that
// has the same methods, but just returns values we choose.
//
// The fake also RECORDS how it was called (query, page, size), so the
// tests can check that the service passed the right values down.
type fakeRepository struct {
	// What the fake will return from Search
	searchItems []domain.VariantStock
	searchTotal int
	searchErr   error

	// What the fake will return from Adjust
	adjustErr error

	// What the fake saw when Search was called (filled in during the test)
	searchQuery  string
	searchPage   int
	searchSize   int
	searchCalled bool // true if Search was called at all
}

// Search is the fake version of the repository's Search method.
// It saves the arguments it received, then returns the preset values.
func (f *fakeRepository) Search(
	_ context.Context, // "_" means we ignore this argument
	query string,
	page int,
	size int,
) ([]domain.VariantStock, int, error) {
	f.searchCalled = true
	f.searchQuery = query
	f.searchPage = page
	f.searchSize = size

	return f.searchItems, f.searchTotal, f.searchErr
}

// Adjust is the fake version of the repository's Adjust method.
// It ignores all inputs and only returns the error we set up (or nil).
func (f *fakeRepository) Adjust(
	_ context.Context,
	_ int,
	_ int,
	_ int,
	_ string,
) error {
	return f.adjustErr
}

// ---------------------------------------------------------------
// TEST 1: Search cleans the query and passes pagination along
// ---------------------------------------------------------------
// Checks that when we search "  BB-1001  " (with extra spaces),
// the service trims it to "BB-1001" before calling the repository,
// and passes page and size unchanged.
func TestSearchTrimsQueryAndPassesPagination(t *testing.T) {
	// Set up a fake that will return one product and a total of 1
	repository := &fakeRepository{
		searchItems: []domain.VariantStock{
			{
				VariantID:     1,
				SKU:           "BB-1001",
				ProductName:   "Test Product",
				StockQuantity: 7,
			},
		},
		searchTotal: 1,
	}

	// Create the real service, but give it our fake repository
	service := NewService(repository)

	// Call Search with a query that has spaces around it
	items, total, err := service.Search(
		context.Background(),
		"  BB-1001  ",
		2,  // page
		10, // size
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	// The service must have called the repository
	if !repository.searchCalled {
		t.Fatal("repository Search was not called")
	}

	// The spaces must be removed
	if repository.searchQuery != "BB-1001" {
		t.Fatalf(
			"query = %q, want %q",
			repository.searchQuery,
			"BB-1001",
		)
	}

	// Page and size must reach the repository unchanged
	if repository.searchPage != 2 {
		t.Fatalf("page = %d, want 2", repository.searchPage)
	}

	if repository.searchSize != 10 {
		t.Fatalf("size = %d, want 10", repository.searchSize)
	}

	// The results from the repository must come back to the caller
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
}

// ---------------------------------------------------------------
// TEST 2: Search rejects bad page / size values
// ---------------------------------------------------------------
// This is a "table-driven test": we list many cases in a table and
// run the same checks on each one.
// Rules being tested: page must be >= 1, and size must be 1 to 100.
func TestSearchRejectsInvalidPagination(t *testing.T) {
	// Each row is one test case: a name plus the bad inputs
	tests := []struct {
		name string
		page int
		size int
	}{
		{name: "page is zero", page: 0, size: 20},
		{name: "page is negative", page: -1, size: 20},
		{name: "size is zero", page: 1, size: 0},
		{name: "size is negative", page: 1, size: -1},
		{name: "size is greater than maximum", page: 1, size: 101},
	}

	// Run every case as its own sub-test
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{}
			service := NewService(repository)

			_, _, err := service.Search(
				context.Background(),
				"", // empty query is fine here
				test.page,
				test.size,
			)

			// The service must return ErrInvalidRequest
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf(
					"got error %v, want ErrInvalidRequest",
					err,
				)
			}

			// And it must stop BEFORE touching the repository
			if repository.searchCalled {
				t.Fatal("repository should not be called for invalid pagination")
			}
		})
	}
}

// ---------------------------------------------------------------
// TEST 3: Search passes database errors up
// ---------------------------------------------------------------
// If the repository fails (for example, the database is down),
// the service should return that same error, not hide it.
func TestSearchPassesRepositoryError(t *testing.T) {
	expectedErr := errors.New("database unavailable")

	// Make the fake return an error
	repository := &fakeRepository{
		searchErr: expectedErr,
	}

	service := NewService(repository)

	// Valid page and size, so the call reaches the repository
	_, _, err := service.Search(
		context.Background(),
		"",
		1,
		20,
	)

	// errors.Is checks that our expected error is the one returned
	if !errors.Is(err, expectedErr) {
		t.Fatalf("got error %v, want %v", err, expectedErr)
	}
}

// ---------------------------------------------------------------
// TEST 4: Adjust rejects an invalid request
// ---------------------------------------------------------------
// The last argument (the reason) is an empty string, and the
// quantity change is 1. The service should say the request is
// invalid, most likely because the reason is missing.
func TestAdjustRejectsInvalidRequest(t *testing.T) {
	service := NewService(&fakeRepository{})

	err := service.Adjust(
		context.Background(),
		1,  // first ID (e.g. variant ID)
		1,  // second ID (e.g. user ID)
		1,  // amount to change stock by
		"", // reason is empty -> invalid
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("got %v, want ErrInvalidRequest", err)
	}
}

// ---------------------------------------------------------------
// TEST 5: Adjust accepts a valid request
// ---------------------------------------------------------------
// A normal restock (+20) with a reason should succeed with no error.
func TestAdjustAcceptsValidRequest(t *testing.T) {
	service := NewService(&fakeRepository{})

	err := service.Adjust(
		context.Background(),
		1,
		1,
		20,        // add 20 items
		"restock", // a clear reason
	)
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

// ---------------------------------------------------------------
// TEST 6: Adjust passes the "below zero" error up
// ---------------------------------------------------------------
// If removing 5 items would make stock negative, the repository
// returns ErrAdjustmentBelowZero. The service must pass that
// exact error back so the caller knows what went wrong.
func TestAdjustPassesBelowZeroError(t *testing.T) {
	// Make the fake pretend the stock would go below zero
	service := NewService(&fakeRepository{
		adjustErr: ErrAdjustmentBelowZero,
	})

	err := service.Adjust(
		context.Background(),
		1,
		1,
		-5, // remove 5 items
		"correction",
	)
	if !errors.Is(err, ErrAdjustmentBelowZero) {
		t.Fatalf(
			"got %v, want ErrAdjustmentBelowZero",
			err,
		)
	}
}
