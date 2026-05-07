package bili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// mixinKeyEncTab is bilibili's permutation of the 64-char (img_key + sub_key)
// pair used to derive the WBI signing key.
//
// Source: https://github.com/SocialSisterYi/bilibili-API-collect/blob/master/docs/misc/sign/wbi.md
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

type wbiKeys struct {
	imgKey, subKey string
	fetchedAt      time.Time
}

func (k wbiKeys) mixinKey() string {
	src := k.imgKey + k.subKey
	if len(src) < 64 {
		return ""
	}
	var b strings.Builder
	for _, i := range mixinKeyEncTab {
		if i < len(src) {
			b.WriteByte(src[i])
		}
	}
	out := b.String()
	if len(out) < 32 {
		return out
	}
	return out[:32]
}

// wbiCache holds the most recently fetched img/sub keys. They typically rotate
// every ~12 hours; a short TTL keeps us safe without hammering /nav.
const wbiTTL = 5 * time.Minute

func (c *Client) getWBIKeys(ctx context.Context) (wbiKeys, error) {
	c.wbiMu.RLock()
	cached := c.wbi
	c.wbiMu.RUnlock()
	if cached.imgKey != "" && time.Since(cached.fetchedAt) < wbiTTL {
		return cached, nil
	}

	c.wbiMu.Lock()
	defer c.wbiMu.Unlock()
	// Re-check after taking the write lock to avoid duplicate fetches under contention.
	if c.wbi.imgKey != "" && time.Since(c.wbi.fetchedAt) < wbiTTL {
		return c.wbi, nil
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			WbiImg struct {
				ImgURL string `json:"img_url"`
				SubURL string `json:"sub_url"`
			} `json:"wbi_img"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "https://api.bilibili.com/x/web-interface/nav", &resp); err != nil {
		return wbiKeys{}, err
	}
	c.wbi = wbiKeys{
		imgKey:    keyFromURL(resp.Data.WbiImg.ImgURL),
		subKey:    keyFromURL(resp.Data.WbiImg.SubURL),
		fetchedAt: time.Now(),
	}
	if c.wbi.imgKey == "" || c.wbi.subKey == "" {
		return wbiKeys{}, fmt.Errorf("nav returned empty wbi keys (code=%d)", resp.Code)
	}
	return c.wbi, nil
}

func keyFromURL(u string) string {
	if u == "" {
		return ""
	}
	base := path.Base(u)
	if i := strings.IndexByte(base, '.'); i > 0 {
		return base[:i]
	}
	return base
}

// SignWBI adds wts and w_rid signing parameters to the values map.
//
// Bilibili's algorithm (per bilibili-API-collect):
//  1. Set wts = current unix time.
//  2. Strip "!'()*" from each value.
//  3. URL-encode and sort all params alphabetically by key.
//  4. w_rid = md5(canonical_query + mixin_key).
func (c *Client) SignWBI(ctx context.Context, values url.Values) error {
	keys, err := c.getWBIKeys(ctx)
	if err != nil {
		return err
	}
	mixin := keys.mixinKey()

	values.Set("wts", fmt.Sprintf("%d", time.Now().Unix()))

	keysSorted := make([]string, 0, len(values))
	for k := range values {
		keysSorted = append(keysSorted, k)
	}
	sort.Strings(keysSorted)

	var query strings.Builder
	for i, k := range keysSorted {
		if i > 0 {
			query.WriteByte('&')
		}
		query.WriteString(url.QueryEscape(k))
		query.WriteByte('=')
		query.WriteString(url.QueryEscape(stripWBIChars(values.Get(k))))
	}

	sum := md5.Sum([]byte(query.String() + mixin))
	values.Set("w_rid", hex.EncodeToString(sum[:]))
	return nil
}

func stripWBIChars(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '!', '\'', '(', ')', '*':
			return -1
		}
		return r
	}, s)
}

