package files

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chhongzh/atri-bot/internal/config"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestUploadStoresOpaqueReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/files" || request.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected upload request: %s %q", request.URL.Path, request.Header.Get("Authorization"))
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if request.FormValue("purpose") != "user_data" {
			t.Fatalf("unexpected purpose %q", request.FormValue("purpose"))
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		if header.Filename != "photo.jpg" || string(body) != "image" {
			t.Fatalf("unexpected file %q %q", header.Filename, body)
		}
		_, _ = writer.Write([]byte(`{"id":"provider-file"}`))
	}))
	defer server.Close()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop(), server.Client())
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	settings := config.UserSettings{AIBaseURL: server.URL, AIAPIKey: "key", AIConfigRevision: 2}
	ref, err := manager.Upload(context.Background(), settings, 1, "character", "image", "photo.jpg", io.NopCloser(strings.NewReader("image")), 5)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := manager.IDs(context.Background(), 1, "character", 2, []string{ref.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "provider-file" {
		t.Fatalf("unexpected ids %#v", ids)
	}
	ids, err = manager.IDs(context.Background(), 2, "character", 2, []string{ref.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("cross-user reference leaked: %#v", ids)
	}
}
