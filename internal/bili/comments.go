package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type Comment struct {
	ID        int64
	Author    string
	AvatarURL string
	Text      string
	Likes     int64
	Posted    time.Time
	Replies   int // number of nested replies (not fetched in v1)
}

// VideoComments returns top-level comments for a video, sorted by hotness.
// oid is the video's aid (not bvid).
func (c *Client) VideoComments(ctx context.Context, oid int64) ([]Comment, error) {
	q := url.Values{}
	q.Set("type", "1") // 1 = video
	q.Set("oid", strconv.FormatInt(oid, 10))
	q.Set("mode", "3") // sort by hotness (default on bilibili)
	q.Set("next", "0")
	endpoint := "https://api.bilibili.com/x/v2/reply/main?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Replies []struct {
				RpID    int64 `json:"rpid"`
				Like    int64 `json:"like"`
				Ctime   int64 `json:"ctime"`
				Rcount  int   `json:"rcount"`
				Content struct {
					Message string `json:"message"`
				} `json:"content"`
				Member struct {
					Uname  string `json:"uname"`
					Avatar string `json:"avatar"`
				} `json:"member"`
			} `json:"replies"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("comments: code=%d msg=%q", resp.Code, resp.Msg)
	}
	out := make([]Comment, 0, len(resp.Data.Replies))
	for _, r := range resp.Data.Replies {
		out = append(out, Comment{
			ID:        r.RpID,
			Author:    r.Member.Uname,
			AvatarURL: r.Member.Avatar,
			Text:      r.Content.Message,
			Likes:     r.Like,
			Posted:    time.Unix(r.Ctime, 0),
			Replies:   r.Rcount,
		})
	}
	return out, nil
}
