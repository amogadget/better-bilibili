package bili

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// FeedItem is one entry in the user's video dynamic feed (subscriptions).
type FeedItem struct {
	BVID      string
	Title     string
	Author    string
	AuthorID  int64
	ThumbURL  string
	Duration  string // "mm:ss" string from bilibili
	Published time.Time
}

type FeedPage struct {
	Items   []FeedItem
	Offset  string // pass back as ?offset= for the next page
	HasMore bool
}

// VideoFeed fetches the user's video-only dynamic feed. offset is the cursor
// returned by a previous call; empty for first page.
func (c *Client) VideoFeed(ctx context.Context, offset string) (*FeedPage, error) {
	q := url.Values{}
	q.Set("type", "video")
	q.Set("page", "1")
	if offset != "" {
		q.Set("offset", offset)
	}
	endpoint := "https://api.bilibili.com/x/polymer/web-dynamic/v1/feed/all?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			HasMore bool   `json:"has_more"`
			Offset  string `json:"offset"`
			Items   []struct {
				Type    string `json:"type"`
				IDStr   string `json:"id_str"`
				Modules struct {
					Author struct {
						Name    string `json:"name"`
						MID     int64  `json:"mid"`
						PubTS   int64  `json:"pub_ts"`
					} `json:"module_author"`
					Dynamic struct {
						Major struct {
							Type    string `json:"type"`
							Archive struct {
								BVID         string `json:"bvid"`
								Title        string `json:"title"`
								Cover        string `json:"cover"`
								DurationText string `json:"duration_text"`
							} `json:"archive"`
						} `json:"major"`
					} `json:"module_dynamic"`
				} `json:"modules"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("dynamic feed: code=%d msg=%q", resp.Code, resp.Msg)
	}

	page := &FeedPage{Offset: resp.Data.Offset, HasMore: resp.Data.HasMore}
	for _, it := range resp.Data.Items {
		if it.Modules.Dynamic.Major.Type != "MAJOR_TYPE_ARCHIVE" {
			continue
		}
		a := it.Modules.Dynamic.Major.Archive
		if a.BVID == "" {
			continue
		}
		page.Items = append(page.Items, FeedItem{
			BVID:      a.BVID,
			Title:     a.Title,
			Author:    it.Modules.Author.Name,
			AuthorID:  it.Modules.Author.MID,
			ThumbURL:  normalizeURL(a.Cover),
			Duration:  a.DurationText,
			Published: time.Unix(it.Modules.Author.PubTS, 0),
		})
	}
	return page, nil
}
