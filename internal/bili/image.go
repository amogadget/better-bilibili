package bili

import (
	"fmt"
	"strings"
)

// ResizeImage rewrites a bilibili-CDN image URL so the upstream returns a
// thumbnail of the requested size instead of the full-source image. Use it
// anywhere a smaller display size would otherwise pull a multi-MB original.
//
// Format: appends "@<W>w_<H>h_1c.webp". The `_1c` flag center-crops to the
// exact dimensions; drop it if you need aspect-preserving fit instead.
//
// Non-hdslb.com URLs are returned unchanged so this is safe to call on any
// URL the templates carry.
func ResizeImage(url string, w, h int) string {
	if url == "" || !strings.Contains(url, "hdslb.com") {
		return url
	}
	return fmt.Sprintf("%s@%dw_%dh_1c.webp", url, w, h)
}
