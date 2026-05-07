package bili

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// SearchResult is one video hit from bilibili search.
type SearchResult struct {
	BVID        string
	Title       string // HTML stripped of bilibili's <em class="keyword"> highlight tags
	Author      string
	UploaderID  int64
	ThumbURL    string // normalized to https
	Duration    string // mm:ss or hh:mm:ss
	PlayCount   int64
	Description string
}

// SearchVideos calls the bilibili search/type endpoint with search_type=video.
func (c *Client) SearchVideos(ctx context.Context, keyword string, page int) ([]SearchResult, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("search_type", "video")
	q.Set("keyword", keyword)
	q.Set("page", fmt.Sprintf("%d", page))
	endpoint := "https://api.bilibili.com/x/web-interface/search/type?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Result []struct {
				Type        string `json:"type"`
				BVID        string `json:"bvid"`
				Title       string `json:"title"`
				Author      string `json:"author"`
				MID         int64  `json:"mid"`
				Pic         string `json:"pic"`
				Duration    string `json:"duration"`
				Play        int64  `json:"play"`
				Description string `json:"description"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("search: code=%d msg=%q", resp.Code, resp.Msg)
	}

	out := make([]SearchResult, 0, len(resp.Data.Result))
	for _, r := range resp.Data.Result {
		if r.Type != "" && r.Type != "video" {
			continue
		}
		out = append(out, SearchResult{
			BVID:        r.BVID,
			Title:       cleanHighlight(r.Title),
			Author:      r.Author,
			UploaderID:  r.MID,
			ThumbURL:    normalizeURL(r.Pic),
			Duration:    r.Duration,
			PlayCount:   r.Play,
			Description: r.Description,
		})
	}
	return out, nil
}

var highlightRE = regexp.MustCompile(`</?em[^>]*>`)

func cleanHighlight(s string) string {
	return highlightRE.ReplaceAllString(s, "")
}

// normalizeURL turns "//i2.hdslb.com/..." into "https://i2.hdslb.com/..."
func normalizeURL(u string) string {
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
