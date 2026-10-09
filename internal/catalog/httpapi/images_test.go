package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/auth"
)

type imageStorageStub struct {
	openPayload []byte
}

func (imageStorageStub) PresignUpload(context.Context, string, string, time.Duration) (string, error) {
	return "https://minio.test/upload", nil
}
func (s imageStorageStub) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.openPayload)), nil
}
func (imageStorageStub) Put(context.Context, string, string, int64, io.Reader) error { return nil }
func (imageStorageStub) Delete(context.Context, string) error                        { return nil }

type imageRepositoryStub struct {
	createErr error
	deleteErr error
}

func (imageRepositoryStub) ProductExists(context.Context, int64) (bool, error) { return true, nil }
func (s imageRepositoryStub) CreateImage(context.Context, domain.Image) (domain.Image, error) {
	return domain.Image{ID: 9}, s.createErr
}
func (s imageRepositoryStub) DeleteImage(context.Context, int64, int64) (string, error) {
	return "products/1/image", s.deleteErr
}

func imageTestRouter() chi.Router {
	r := chi.NewRouter()
	handler := NewImageHandler(app.NewImageService(imageStorageStub{}, imageRepositoryStub{}))
	RegisterImageRoutes(r, handler)
	return r
}

func TestImageRoutesRequireImagePermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/staff/products/1/images/upload-url", bytes.NewBufferString(`{"content_type":"image/png"}`))
	response := httptest.NewRecorder()
	imageTestRouter().ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestImagePresignRequiresDistinctImagePermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/staff/products/1/images/upload-url", bytes.NewBufferString(`{"content_type":"image/png"}`))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{Permissions: []string{"catalog:write"}}))
	response := httptest.NewRecorder()
	imageTestRouter().ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestImagePresignReturnsUploadURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/staff/products/1/images/upload-url", bytes.NewBufferString(`{"content_type":"image/png"}`))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{Permissions: []string{"catalog:image:write"}}))
	response := httptest.NewRecorder()
	imageTestRouter().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestImageConfirmReturnsCreated(t *testing.T) {
	storage := imageStorageStub{openPayload: pngHTTPFixture(t)}
	handler := NewImageHandler(app.NewImageService(storage, imageRepositoryStub{}))
	router := chi.NewRouter()
	RegisterImageRoutes(router, handler)
	req := authorizedRequest(http.MethodPost, "/api/v1/staff/products/1/images", `{"object_key":"products/1/upload","content_type":"image/png"}`, "catalog:image:write")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestImageConfirmMapsInvalidMetadataToBadRequest(t *testing.T) {
	req := authorizedRequest(http.MethodPost, "/api/v1/staff/products/1/images", `{"object_key":"wrong-prefix","content_type":"image/png"}`, "catalog:image:write")
	response := httptest.NewRecorder()
	imageTestRouter().ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func TestImageDeleteReturnsNoContent(t *testing.T) {
	router := chi.NewRouter()
	handler := NewImageHandler(app.NewImageService(imageStorageStub{}, imageRepositoryStub{}))
	RegisterImageRoutes(router, handler)
	req := authorizedRequest(http.MethodDelete, "/api/v1/staff/products/1/images/9", "", "catalog:image:write")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
}

func TestImageDeleteMapsNotFound(t *testing.T) {
	router := chi.NewRouter()
	handler := NewImageHandler(app.NewImageService(imageStorageStub{}, imageRepositoryStub{deleteErr: domain.ErrNotFound}))
	RegisterImageRoutes(router, handler)
	req := authorizedRequest(http.MethodDelete, "/api/v1/staff/products/1/images/9", "", "catalog:image:write")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}

func pngHTTPFixture(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	fixture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	fixture.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&output, fixture); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
