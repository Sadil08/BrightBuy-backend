package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/dbx"
)

func cleanupProduct(t *testing.T, db *sql.DB, productID int64) {
	t.Helper()

	_, err := db.Exec(
		`DELETE FROM product WHERE product_id = ?`,
		productID,
	)
	if err != nil {
		t.Fatalf("cleanup product %d: %v", productID, err)
	}
}

func cleanupCategory(t *testing.T, db *sql.DB, categoryID int64) {
	t.Helper()

	_, err := db.Exec(
		`DELETE FROM category WHERE category_id = ?`,
		categoryID,
	)
	if err != nil {
		t.Fatalf("cleanup category %d: %v", categoryID, err)
	}
}

func uniqueValue(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func repoTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("BRIGHTBUY_TEST_DSN")
	if dsn == "" {
		dsn = "brightbuy_app:devapppass@tcp(127.0.0.1:3308)/brightbuy?parseTime=true"
	}

	db, err := dbx.Open(dsn)
	if err != nil {
		t.Skipf("integration database unavailable: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestCreateProduct_Integration(t *testing.T) {
	db := repoTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	// Create a category first.
	category, err := repo.CreateCategory(ctx, app.CategoryInput{
		Name:        uniqueValue("Integration-Category"),
		Description: "Created by integration test",
	})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}
	t.Cleanup(func() { cleanupCategory(t, db, category.ID) })
	productNameExpected := uniqueValue("Integration-Product")
	variantSKUExpected := uniqueValue("INTEGRATION-SKU")

	// Create a product with one variant and the category.
	product, err := repo.CreateProduct(
		ctx,
		app.AdminProduct{
			Name:        productNameExpected,
			Description: "Created by integration test",
			Active:      true,
		},
		[]int64{category.ID},
		[]app.VariantInput{
			{
				SKU:           variantSKUExpected,
				PriceCents:    1999,
				StockQuantity: 10,
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}

	if product.ID == 0 {
		t.Fatal("CreateProduct() returned product with ID 0")
	}
	t.Cleanup(func() { cleanupProduct(t, db, product.ID) })

	// Verify the product exists in MySQL.
	var productNameActual string
	err = db.QueryRowContext(
		ctx,
		`SELECT name FROM product WHERE product_id = ?`,
		product.ID,
	).Scan(&productNameActual)

	if err != nil {
		t.Fatalf("query created product: %v", err)
	}

	if productNameActual != productNameExpected {
		t.Errorf("product name = %q, want %q", productNameActual, productNameExpected)
	}

	// Verify the category relationship exists.
	var categoryCount int
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM product_category
		 WHERE product_id = ? AND category_id = ?`,
		product.ID,
		category.ID,
	).Scan(&categoryCount)

	if err != nil {
		t.Fatalf("query product category relationship: %v", err)
	}

	if categoryCount != 1 {
		t.Errorf("category relationship count = %d, want 1", categoryCount)
	}

	// Verify the variant exists.
	var variantCount int
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM product_variant
		 WHERE product_id = ? AND sku = ?`,
		product.ID,
		variantSKUExpected,
	).Scan(&variantCount)

	if err != nil {
		t.Fatalf("query created variant: %v", err)
	}

	if variantCount != 1 {
		t.Errorf("variant count = %d, want 1", variantCount)
	}
}

func TestCreateVariant_DuplicateSKU_ReturnsConflict(t *testing.T) {
	db := repoTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	// Create a product for the variants.
	product, err := repo.CreateProduct(
		ctx,
		app.AdminProduct{
			Name:   uniqueValue("Duplicate-SKU-Product"),
			Active: true,
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	t.Cleanup(func() { cleanupProduct(t, db, product.ID) })

	input := app.VariantInput{
		SKU:           uniqueValue("DUPLICATE-SKU"),
		PriceCents:    2500,
		StockQuantity: 5,
	}

	// First variant should succeed.
	_, err = repo.CreateVariant(ctx, product.ID, input)
	if err != nil {
		t.Fatalf("first CreateVariant() error = %v", err)
	}

	// Second variant uses the same SKU.
	_, err = repo.CreateVariant(ctx, product.ID, input)

	if !errors.Is(err, domain.ErrSKUConflict) {
		t.Fatalf(
			"second CreateVariant() error = %v, want %v",
			err,
			domain.ErrSKUConflict,
		)
	}
}

func TestCreateProduct_InvalidCategory_RollsBack(t *testing.T) {
	db := repoTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	invalidCategoryID := int64(999999999)

	_, err := repo.CreateProduct(
		ctx,
		app.AdminProduct{
			Name:        "Rollback Test Product",
			Description: "This product should be rolled back",
			Active:      true,
		},
		[]int64{invalidCategoryID},
		[]app.VariantInput{
			{
				SKU:           "ROLLBACK-CATEGORY-001",
				PriceCents:    1000,
				StockQuantity: 5,
			},
		},
	)

	if err == nil {
		t.Fatal("CreateProduct() expected an error, got nil")
	}

	// The product must not exist because the transaction should have rolled back.
	var count int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM product WHERE name = ?`,
		"Rollback Test Product",
	).Scan(&count)

	if err != nil {
		t.Fatalf("checking rollback: %v", err)
	}

	if count != 0 {
		t.Fatalf("product still exists after rollback, count = %d", count)
	}

	// The variant must also not exist.
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM product_variant WHERE sku = ?`,
		"ROLLBACK-CATEGORY-001",
	).Scan(&count)

	if err != nil {
		t.Fatalf("checking variant rollback: %v", err)
	}

	if count != 0 {
		t.Fatalf("variant still exists after rollback, count = %d", count)
	}
}

func TestCreateProduct_InvalidVariant_RollsBack(t *testing.T) {
	db := repoTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	_, err := repo.CreateProduct(
		ctx,
		app.AdminProduct{
			Name:        "Variant Rollback Test Product",
			Description: "This product should be rolled back",
			Active:      true,
		},
		nil,
		[]app.VariantInput{
			{
				SKU:           "ROLLBACK-VARIANT-001",
				PriceCents:    1000,
				StockQuantity: -1,
			},
		},
	)

	if err == nil {
		t.Fatal("CreateProduct() expected an error, got nil")
	}

	// The product must not exist.
	var count int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM product WHERE name = ?`,
		"Variant Rollback Test Product",
	).Scan(&count)

	if err != nil {
		t.Fatalf("checking product rollback: %v", err)
	}

	if count != 0 {
		t.Fatalf("product still exists after rollback, count = %d", count)
	}

	// The invalid variant must not exist either.
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM product_variant WHERE sku = ?`,
		"ROLLBACK-VARIANT-001",
	).Scan(&count)

	if err != nil {
		t.Fatalf("checking variant rollback: %v", err)
	}

	if count != 0 {
		t.Fatalf("variant still exists after rollback, count = %d", count)
	}
}

func TestProduct_UpdateAndDeactivate_Integration(t *testing.T) {
	db := repoTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	product, err := repo.CreateProduct(
		ctx,
		app.AdminProduct{
			Name:        uniqueValue("Original-Product"),
			Description: "Original description",
			Active:      true,
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	t.Cleanup(func() { cleanupProduct(t, db, product.ID) })

	newName := uniqueValue("Updated-Product")
	newDescription := "Updated description"

	err = repo.UpdateProduct(ctx, product.ID, app.ProductPatch{
		Name:        &newName,
		Description: &newDescription,
	})
	if err != nil {
		t.Fatalf("UpdateProduct() error = %v", err)
	}

	var name, description string
	var active bool

	err = db.QueryRowContext(
		ctx,
		`SELECT name, description, is_active
		 FROM product
		 WHERE product_id = ?`,
		product.ID,
	).Scan(&name, &description, &active)

	if err != nil {
		t.Fatalf("query updated product: %v", err)
	}

	if name != newName {
		t.Errorf("name = %q, want %q", name, newName)
	}

	if description != newDescription {
		t.Errorf("description = %q, want %q", description, newDescription)
	}

	if !active {
		t.Error("product unexpectedly became inactive after update")
	}

	// Deactivate instead of deleting.
	err = repo.SetProductActive(ctx, product.ID, false)
	if err != nil {
		t.Fatalf("SetProductActive() error = %v", err)
	}

	err = db.QueryRowContext(
		ctx,
		`SELECT is_active FROM product WHERE product_id = ?`,
		product.ID,
	).Scan(&active)

	if err != nil {
		t.Fatalf("query product active state: %v", err)
	}

	if active {
		t.Error("product is still active after deactivation")
	}
}

func TestVariant_UpdateAndDeactivate_Integration(t *testing.T) {
	db := repoTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	// Create a product to own the variant.
	product, err := repo.CreateProduct(
		ctx,
		app.AdminProduct{
			Name:   uniqueValue("Variant-Update-Product"),
			Active: true,
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	t.Cleanup(func() { cleanupProduct(t, db, product.ID) })

	// Create the original variant.
	variant, err := repo.CreateVariant(ctx, product.ID, app.VariantInput{
		SKU:           uniqueValue("VARIANT-UPDATE"),
		PriceCents:    1500,
		StockQuantity: 10,
	})
	if err != nil {
		t.Fatalf("CreateVariant() error = %v", err)
	}

	// Update the variant.
	newSKU := uniqueValue("VARIANT-UPDATED")
	newPrice := int64(2500)
	newStock := 20

	err = repo.UpdateVariant(ctx, variant.ID, app.VariantPatch{
		SKU:           &newSKU,
		PriceCents:    &newPrice,
		StockQuantity: &newStock,
	})
	if err != nil {
		t.Fatalf("UpdateVariant() error = %v", err)
	}

	// Verify the updated values in MySQL.
	var sku string
	var price string
	var stock int
	var active bool

	err = db.QueryRowContext(
		ctx,
		`SELECT sku, price, stock_quantity, is_active
		 FROM product_variant
		 WHERE variant_id = ?`,
		variant.ID,
	).Scan(&sku, &price, &stock, &active)

	if err != nil {
		t.Fatalf("query updated variant: %v", err)
	}

	if sku != newSKU {
		t.Errorf("SKU = %q, want %q", sku, newSKU)
	}

	if price != "25.00" {
		t.Errorf("price = %q, want 25.00", price)
	}

	if stock != newStock {
		t.Errorf("stock = %d, want %d", stock, newStock)
	}

	if !active {
		t.Error("variant unexpectedly became inactive")
	}

	// Deactivate the variant without deleting it.
	err = repo.SetVariantActive(ctx, variant.ID, false)
	if err != nil {
		t.Fatalf("SetVariantActive() error = %v", err)
	}

	err = db.QueryRowContext(
		ctx,
		`SELECT is_active
		 FROM product_variant
		 WHERE variant_id = ?`,
		variant.ID,
	).Scan(&active)

	if err != nil {
		t.Fatalf("query variant active state: %v", err)
	}

	if active {
		t.Error("variant is still active after deactivation")
	}
}

func TestCategory_UpdateAndDeactivate_Integration(t *testing.T) {
	db := repoTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	// Create the original category.
	category, err := repo.CreateCategory(ctx, app.CategoryInput{
		Name:        uniqueValue("Original-Category"),
		Description: "Original description",
	})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}
	t.Cleanup(func() { cleanupCategory(t, db, category.ID) })

	// Update the category.
	newName := uniqueValue("Updated-Category")
	newDescription := "Updated description"

	err = repo.UpdateCategory(ctx, category.ID, app.CategoryPatch{
		Name:        &newName,
		Description: &newDescription,
	})
	if err != nil {
		t.Fatalf("UpdateCategory() error = %v", err)
	}

	// Verify the update in MySQL.
	var name string
	var description string
	var active bool

	err = db.QueryRowContext(
		ctx,
		`SELECT name, description, is_active
		 FROM category
		 WHERE category_id = ?`,
		category.ID,
	).Scan(&name, &description, &active)

	if err != nil {
		t.Fatalf("query updated category: %v", err)
	}

	if name != newName {
		t.Errorf("name = %q, want %q", name, newName)
	}

	if description != newDescription {
		t.Errorf("description = %q, want %q", description, newDescription)
	}

	if !active {
		t.Error("category unexpectedly became inactive after update")
	}

	// Deactivate instead of deleting.
	err = repo.SetCategoryActive(ctx, category.ID, false)
	if err != nil {
		t.Fatalf("SetCategoryActive() error = %v", err)
	}

	err = db.QueryRowContext(
		ctx,
		`SELECT is_active
		 FROM category
		 WHERE category_id = ?`,
		category.ID,
	).Scan(&active)

	if err != nil {
		t.Fatalf("query category active state: %v", err)
	}

	if active {
		t.Error("category is still active after deactivation")
	}
}
