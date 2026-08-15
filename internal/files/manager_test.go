package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chhongzh/atri-bot/internal/constants"
	"go.uber.org/zap"
)

func TestSaveLoadAndQuota(t *testing.T) {
	manager := New(context.Background(), t.TempDir(), 8, constants.DefaultFilesCleanupAge, zap.NewNop())
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	ref, err := manager.Save(context.Background(), "video", "video.mp4", 1024, io.NopCloser(strings.NewReader("video")), 5)
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := manager.Load(context.Background(), []string{ref.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 || attachments[0].Kind != "video" || attachments[0].Base64 != base64.StdEncoding.EncodeToString([]byte("video")) {
		t.Fatalf("unexpected attachments %#v", attachments)
	}
	duplicate, err := manager.Save(context.Background(), "video", "copy.mp4", 1024, io.NopCloser(strings.NewReader("video")), 5)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != ref.ID {
		t.Fatalf("duplicate content returned different refs: %q != %q", duplicate.ID, ref.ID)
	}
	if _, err = manager.Save(context.Background(), "video", "other.mp4", 1024, io.NopCloser(strings.NewReader("other")), 5); err == nil {
		t.Fatal("expected global pool quota error")
	}
	used, err := directorySize(manager.root)
	if err != nil {
		t.Fatal(err)
	}
	if used != 5 {
		t.Fatalf("duplicate content occupied extra space: %d", used)
	}
}

func TestSaveResizesImageBeforeStorage(t *testing.T) {
	manager := New(context.Background(), t.TempDir(), constants.DefaultFilesStorageBytes, constants.DefaultFilesCleanupAge, zap.NewNop())
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	source := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	draw.Draw(source, source.Bounds(), &image.Uniform{C: color.RGBA{R: 200, G: 100, B: 50, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	ref, err := manager.Save(context.Background(), "image", "photo.png", 1024, io.NopCloser(bytes.NewReader(encoded.Bytes())), int64(encoded.Len()))
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := manager.Load(context.Background(), []string{ref.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 || attachments[0].MIMEType != "image/jpeg" {
		t.Fatalf("unexpected attachments %#v", attachments)
	}
	data, err := base64.StdEncoding.DecodeString(attachments[0].Base64)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Width != 1024 || configuration.Height != 512 {
		t.Fatalf("resized image dimensions = %dx%d", configuration.Width, configuration.Height)
	}
}

func TestResizeImageOnlyShrinksOversizedImages(t *testing.T) {
	for _, test := range []struct {
		input image.Point
		want  image.Point
	}{
		{input: image.Pt(2000, 1000), want: image.Pt(1024, 512)},
		{input: image.Pt(400, 200), want: image.Pt(400, 200)},
	} {
		source := image.NewRGBA(image.Rectangle{Max: test.input})
		draw.Draw(source, source.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		var input bytes.Buffer
		if err := png.Encode(&input, source); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if _, err := resizeImage(&output, &input, 1024); err != nil {
			t.Fatal(err)
		}
		configuration, err := jpeg.DecodeConfig(bytes.NewReader(output.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if configuration.Width != test.want.X || configuration.Height != test.want.Y {
			t.Fatalf("input %dx%d resized to %dx%d", test.input.X, test.input.Y, configuration.Width, configuration.Height)
		}
	}
}

func TestCleanupRemovesFilesOlderThanOneWeek(t *testing.T) {
	root := t.TempDir()
	manager := New(context.Background(), root, constants.DefaultFilesStorageBytes, constants.DefaultFilesCleanupAge, zap.NewNop())
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	ref, err := manager.Save(context.Background(), "video", "video.mp4", 1024, io.NopCloser(strings.NewReader("video")), 5)
	if err != nil {
		t.Fatal(err)
	}
	_, _, hash, _ := parseRef(ref.ID)
	path := filepath.Join(root, hash)
	old := time.Now().Add(-constants.DefaultFilesCleanupAge - time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err = manager.cleanup(); err != nil {
		t.Fatal(err)
	}
	attachments, err := manager.Load(context.Background(), []string{ref.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 0 {
		t.Fatalf("expired attachment was not removed: %#v", attachments)
	}
}
