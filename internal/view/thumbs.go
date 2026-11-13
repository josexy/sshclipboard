package view

import (
	"net"
	"os"
	"strconv"
	"sync"

	"github.com/egoist/mygo/ui"
)

// thumbCache decodes thumbnails off the main thread, so a build pass never
// reads files: get returns nil until the bitmap is ready, then redraws.
type thumbCache struct {
	redraw  func(func())
	mu      sync.Mutex
	bitmaps map[string]*ui.Bitmap
	loading map[string]bool
}

func newThumbCache(redraw func(func())) *thumbCache {
	return &thumbCache{redraw: redraw, bitmaps: map[string]*ui.Bitmap{}, loading: map[string]bool{}}
}

func (tc *thumbCache) get(path string) *ui.Bitmap {
	if path == "" {
		return nil
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if b, ok := tc.bitmaps[path]; ok {
		return b
	}
	if tc.loading[path] || tc.redraw == nil {
		return nil
	}
	tc.loading[path] = true
	go func() {
		var b *ui.Bitmap
		if data, err := os.ReadFile(path); err == nil {
			b, _ = ui.DecodeBitmap(data)
		}
		tc.redraw(func() {
			tc.mu.Lock()
			tc.bitmaps[path] = b // nil: no thumbnail, don't retry
			delete(tc.loading, path)
			tc.mu.Unlock()
		})
	}()
	return nil
}

func hostPort(host string, port int) string { return net.JoinHostPort(host, strconv.Itoa(port)) }
