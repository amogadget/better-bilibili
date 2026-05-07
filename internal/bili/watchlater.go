package bili

import (
	"context"
	"fmt"
)

type WatchLaterItem struct {
	BVID     string
	Title    string
	ThumbURL string
	Duration int // seconds
	Owner    string
}

// WatchLater returns the user's watch-later queue (稍后再看). The endpoint
// returns the full list in one shot; bilibili caps it at ~100 items.
func (c *Client) WatchLater(ctx context.Context) ([]WatchLaterItem, error) {
	const endpoint = "https://api.bilibili.com/x/v2/history/toview"
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			List []struct {
				BVID     string `json:"bvid"`
				Title    string `json:"title"`
				Pic      string `json:"pic"`
				Duration int    `json:"duration"`
				Owner    struct {
					Name string `json:"name"`
				} `json:"owner"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("watch-later: code=%d msg=%q", resp.Code, resp.Msg)
	}
	out := make([]WatchLaterItem, 0, len(resp.Data.List))
	for _, it := range resp.Data.List {
		if it.BVID == "" {
			continue
		}
		out = append(out, WatchLaterItem{
			BVID:     it.BVID,
			Title:    it.Title,
			ThumbURL: normalizeURL(it.Pic),
			Duration: it.Duration,
			Owner:    it.Owner.Name,
		})
	}
	return out, nil
}
