package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// RecommendItem is one card from bilibili's homepage recommendation feed.
type RecommendItem struct {
	BVID     string
	Title    string
	Author   string
	AuthorID int64
	ThumbURL string
	Duration int // seconds
	Views    int64
	Goto     string // "av" for normal videos; live/picture/etc. for things we skip
}

type RecommendPage struct {
	Items []RecommendItem
}

func (c *Client) RecommendVideos(ctx context.Context, freshIdx int) (*RecommendPage, error) {
	if err := c.ensureBuvid3(ctx); err != nil {
		// Don't hard-fail if buvid3 fetch fails; the call may still work.
	}
	if freshIdx < 1 {
		freshIdx = 1
	}
	q := url.Values{}
	q.Set("web_location", "1430650")
	q.Set("y_num", "5")
	q.Set("fresh_type", "4")
	q.Set("feed_version", "V8")
	q.Set("fresh_idx_1h", strconv.Itoa(freshIdx))
	q.Set("fetch_row", "1")
	q.Set("fresh_idx", strconv.Itoa(freshIdx))
	q.Set("brush", strconv.Itoa(freshIdx))
	q.Set("homepage_ver", "1")
	q.Set("ps", "30")
	q.Set("last_y_num", "5")
	q.Set("uniq_id", strconv.FormatInt(int64(freshIdx)*1000+1, 10))

	if err := c.SignWBI(ctx, q); err != nil {
		return nil, fmt.Errorf("wbi sign: %w", err)
	}
	endpoint := "https://api.bilibili.com/x/web-interface/wbi/index/top/feed/rcmd?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Item []struct {
				BVID     string `json:"bvid"`
				Title    string `json:"title"`
				Pic      string `json:"pic"`
				Goto     string `json:"goto"`
				Duration int    `json:"duration"`
				Owner    struct {
					Name string `json:"name"`
					MID  int64  `json:"mid"`
				} `json:"owner"`
				Stat struct {
					View int64 `json:"view"`
				} `json:"stat"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("recommend: code=%d msg=%q", resp.Code, resp.Msg)
	}

	page := &RecommendPage{}
	for _, it := range resp.Data.Item {
		if it.Goto != "" && it.Goto != "av" {
			continue
		}
		if it.BVID == "" {
			continue
		}
		page.Items = append(page.Items, RecommendItem{
			BVID:     it.BVID,
			Title:    it.Title,
			Author:   it.Owner.Name,
			AuthorID: it.Owner.MID,
			ThumbURL: normalizeURL(it.Pic),
			Duration: it.Duration,
			Views:    it.Stat.View,
			Goto:     it.Goto,
		})
	}
	return page, nil
}

// ensureBuvid3 hits the public site once if SESSDATA is set but no buvid3 has
// ever been observed. The cookie is required by some web endpoints (notably
// the homepage rcmd feed) before they will return content.
func (c *Client) ensureBuvid3(ctx context.Context) error {
	if c.cfg.GetCookies().SESSDATA == "" {
		return nil
	}
	// We don't currently persist buvid3, so just re-harvest each cold start by
	// pinging the spi endpoint and including its Set-Cookie on subsequent calls
	// implicitly via cookies the http.Client jar. Today we have no jar wired,
	// so this is a no-op for now — flagged for later if rcmd starts failing.
	_ = ctx
	return nil
}

func FormatDuration(sec int) string {
	if sec <= 0 {
		return ""
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func FormatCount(n int64) string {
	if n < 10000 {
		return strconv.FormatInt(n, 10)
	}
	if n < 100_000_000 {
		f := float64(n) / 10000.0
		if f >= 100 {
			return fmt.Sprintf("%.0f万", f)
		}
		return fmt.Sprintf("%.1f万", f)
	}
	f := float64(n) / 100_000_000.0
	return fmt.Sprintf("%.1f亿", f)
}

