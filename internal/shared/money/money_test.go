package money

import (
	"encoding/json"
	"testing"
)

// TestParseAndString is a table-driven test: instead of one test function per case, we list the
// cases as data (the `tests` slice) and loop over them. This is the standard Go idiom for "same
// logic, many inputs" — adding a new case later is a one-line addition to the table, not a new
// function.
func TestParseAndString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"whole number", "20", "20.00"},
		{"two decimals", "19.99", "19.99"},
		{"single decimal gets padded", "5.5", "5.50"},
		{"zero", "0", "0.00"},
		{"negative", "-3.50", "-3.50"},
	}

	for _, tt := range tests {
		// t.Run registers tt as its own named sub-test, so `go test -run TestParseAndString/negative`
		// can target just one case, and a failure's output names exactly which case failed.
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) returned unexpected error: %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("Parse(%q).String() = %q, want %q", tt.in, got.String(), tt.want)
			}
		})
	}
}

func TestLessThan(t *testing.T) {
	cheap, _ := Parse("9.99")
	expensive, _ := Parse("49.99")

	if !cheap.LessThan(expensive) {
		t.Error("9.99.LessThan(49.99) = false, want true")
	}
	if expensive.LessThan(cheap) {
		t.Error("49.99.LessThan(9.99) = true, want false")
	}
	if cheap.LessThan(cheap) {
		t.Error("9.99.LessThan(9.99) = true, want false (equal is not less than)")
	}
}

func TestParseRejectsMoreThanTwoDecimalPlaces(t *testing.T) {
	if _, err := Parse("19.999"); err == nil {
		t.Fatal("Parse(\"19.999\") returned no error, want an error (3 decimal places is not a valid price)")
	}
}

func TestParseRejectsEmptyString(t *testing.T) {
	if _, err := Parse(""); err == nil {
		t.Fatal("Parse(\"\") returned no error, want an error")
	}
}

// TestJSONRoundTrip checks Money survives being marshaled to JSON and back unchanged — the exact
// path a price takes from Go struct -> HTTP response body -> (on the frontend) back into a request.
func TestJSONRoundTrip(t *testing.T) {
	original, err := Parse("49.99")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(data) != `"49.99"` {
		t.Errorf("json.Marshal(Money) = %s, want a quoted JSON string \"49.99\"", data)
	}

	var decoded Money
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded != original {
		t.Errorf("round-tripped Money = %v, want %v", decoded, original)
	}
}

// TestScanFromDatabaseBytes simulates what go-sql-driver/mysql hands Money.Scan for a DECIMAL(10,2)
// column: a []byte containing the decimal text, e.g. row.Scan(&variant.Price) in the catalog
// repository we're about to write.
func TestScanFromDatabaseBytes(t *testing.T) {
	var m Money
	if err := m.Scan([]byte("49.99")); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if m.String() != "49.99" {
		t.Errorf("after Scan, String() = %q, want %q", m.String(), "49.99")
	}
}
