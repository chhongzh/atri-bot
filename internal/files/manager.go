package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"go.uber.org/zap"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxBytes               int64 = 20 << 20
	DefaultMaxStorageBytes int64 = 1 << 30
	DefaultCleanupAfter          = 7 * 24 * time.Hour
	cleanupInterval              = 24 * time.Hour
	imageQuality                 = 85
)

type Ref struct{ ID string }

type Attachment struct {
	Kind     string
	MIMEType string
	Base64   string
}

type Manager struct {
	root     string
	maxBytes int64
	maxAge   time.Duration
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
}

func New(ctx context.Context, root string, maxBytes int64, maxAge time.Duration, logger *zap.Logger) *Manager {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxStorageBytes
	}
	if maxAge <= 0 {
		maxAge = DefaultCleanupAfter
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{root: root, maxBytes: maxBytes, maxAge: maxAge, logger: logger, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (m *Manager) Init() error {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return err
	}
	if err := m.cleanup(); err != nil {
		return err
	}
	go m.runCleanup()
	return nil
}

func (m *Manager) Close() {
	m.cancel()
	<-m.done
}

func (m *Manager) Save(ctx context.Context, kind, name string, imageMaxEdge int, body io.ReadCloser, declaredSize int64) (Ref, error) {
	defer body.Close()
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	if declaredSize > MaxBytes {
		return Ref{}, fmt.Errorf("媒体文件不能超过 %d MB", MaxBytes>>20)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	used, err := directorySize(m.root)
	if err != nil {
		return Ref{}, err
	}
	temporary, err := os.CreateTemp(m.root, ".upload-*")
	if err != nil {
		return Ref{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	digest := sha256.New()
	var written int64
	var copyErr error
	if kind == "image" {
		written, copyErr = resizeImage(temporary, body, imageMaxEdge)
		if copyErr == nil {
			_, copyErr = temporary.Seek(0, io.SeekStart)
		}
		if copyErr == nil {
			_, copyErr = io.Copy(digest, temporary)
		}
	} else {
		written, copyErr = io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(body, MaxBytes+1))
	}
	closeErr := temporary.Close()
	if copyErr != nil {
		return Ref{}, copyErr
	}
	if closeErr != nil {
		return Ref{}, closeErr
	}
	if written > MaxBytes {
		return Ref{}, fmt.Errorf("媒体文件不能超过 %d MB", MaxBytes>>20)
	}
	if err = ctx.Err(); err != nil {
		return Ref{}, err
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	path := filepath.Join(m.root, hash)
	mimeType := "image/jpeg"
	if kind != "image" {
		mimeType, err = fileMIME(temporaryName, name)
		if err != nil {
			return Ref{}, err
		}
	}
	ref := Ref{ID: kind + "/" + url.PathEscape(mimeType) + "/" + hash}
	if _, err = os.Stat(path); err == nil {
		now := time.Now()
		if err = os.Chtimes(path, now, now); err != nil {
			return Ref{}, err
		}
		return ref, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Ref{}, err
	}
	if used+written > m.maxBytes {
		return Ref{}, fmt.Errorf("本地媒体池已达到 %d MB 上限", m.maxBytes>>20)
	}
	if err = os.Rename(temporaryName, path); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

func resizeImage(destination io.Writer, source io.Reader, maxEdge int) (int64, error) {
	if maxEdge <= 0 {
		maxEdge = configmanager.DefaultImageMaxEdge
	}
	if maxEdge > configmanager.MaxImageMaxEdge {
		return 0, fmt.Errorf("图片最长边不能超过 %d 像素", configmanager.MaxImageMaxEdge)
	}
	data, err := io.ReadAll(io.LimitReader(source, MaxBytes+1))
	if err != nil {
		return 0, err
	}
	if int64(len(data)) > MaxBytes {
		return 0, fmt.Errorf("媒体文件不能超过 %d MB", MaxBytes>>20)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("解码图片: %w", err)
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return 0, errors.New("图片尺寸无效")
	}
	longest := max(width, height)
	output := image.Image(decoded)
	if longest > maxEdge {
		targetWidth := max(1, width*maxEdge/longest)
		targetHeight := max(1, height*maxEdge/longest)
		resized := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		draw.CatmullRom.Scale(resized, resized.Bounds(), decoded, bounds, draw.Over, nil)
		output = resized
	}
	counter := &countingWriter{writer: destination}
	if err = jpeg.Encode(counter, output, &jpeg.Options{Quality: imageQuality}); err != nil {
		return 0, fmt.Errorf("编码图片: %w", err)
	}
	return counter.written, nil
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (w *countingWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.written += int64(written)
	return written, err
}

func (m *Manager) Load(ctx context.Context, refs []string) ([]Attachment, error) {
	attachments := make([]Attachment, 0, len(refs))
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind, mimeType, hash, ok := parseRef(ref)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.root, hash))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > MaxBytes {
			continue
		}
		attachments = append(attachments, Attachment{
			Kind:     kind,
			MIMEType: mimeType,
			Base64:   base64.StdEncoding.EncodeToString(data),
		})
	}
	return attachments, nil
}

func (m *Manager) runCleanup() {
	defer close(m.done)
	interval := min(cleanupInterval, m.maxAge)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := m.cleanup(); err != nil {
				m.logger.Warn("failed to clean expired media files", zap.Error(err))
			}
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-m.maxAge)
	return filepath.WalkDir(m.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			return os.Remove(path)
		}
		return nil
	})
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err == nil {
			size += info.Size()
		}
		return err
	})
	return size, err
}

func parseRef(ref string) (string, string, string, bool) {
	parts := strings.Split(ref, "/")
	if len(parts) != 3 || parts[0] != "image" && parts[0] != "audio" && parts[0] != "video" || len(parts[2]) != sha256.Size*2 {
		return "", "", "", false
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", "", "", false
	}
	mimeType, err := url.PathUnescape(parts[1])
	if err != nil || mimeType == "" {
		return "", "", "", false
	}
	return parts[0], mimeType, parts[2], true
}

func detectMIME(name string, data []byte) string {
	if value := mime.TypeByExtension(filepath.Ext(name)); value != "" {
		return strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
	}
	return http.DetectContentType(data)
}

func fileMIME(path, name string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	prefix := make([]byte, 512)
	read, err := file.Read(prefix)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return detectMIME(name, prefix[:read]), nil
}
