package httpapi

import (
	"testing"

	"brightbuy-backend/internal/catalog/domain"
)

func TestSummaryUsesChosenPreviewImage(t *testing.T) {
	chosen := int64(8)
	product := domain.Product{ID: 1, Images: []domain.ProductImage{{ID: 5, ObjectKey: "a"}, {ID: 8, ObjectKey: "b"}}, PrimaryImageID: &chosen}
	if got := toProductSummaryDTO(product, "").ImageURL; got != "/api/images/8" {
		t.Fatalf("ImageURL = %q, want the chosen image", got)
	}
	product.PrimaryImageID = nil
	if got := toProductSummaryDTO(product, "").ImageURL; got != "/api/images/5" {
		t.Fatalf("ImageURL = %q, want the oldest image", got)
	}
	if got := toProductSummaryDTO(domain.Product{ID: 2}, "").ImageURL; got != "" {
		t.Fatalf("ImageURL = %q, want empty", got)
	}
}

func TestDetailPutsChosenImageFirst(t *testing.T) {
	chosen := int64(8)
	dto := toProductDTO(domain.Product{Images: []domain.ProductImage{{ID: 5}, {ID: 8}}, PrimaryImageID: &chosen}, "")
	if dto.Images[0].ImageID != 8 || !dto.Images[0].IsPrimary || dto.Images[1].IsPrimary {
		t.Fatalf("unexpected order: %+v", dto.Images)
	}
}
