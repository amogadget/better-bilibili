package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HistoryItem is one entry in the user's bilibili-synced watch history.
type HistoryItem struct {
	BVID     string
	AID      int64
	CID      int64
	Title    string
	Author   string
	ThumbURL string
	Duration int       // total seconds
	Progress int       // last position in seconds; -1 means finished
	ViewedAt time.Time // when the user last opened it
}

// HistoryCursor is bilibili's pagination cursor for the history list. Pass
// the previous response's cursor to fetch the next page.
type HistoryCursor struct {
	Max    int64
	ViewAt int64
}

type HistoryPage struct {
	Items   []HistoryItem
	Next    HistoryCursor
	HasMore bool
}

func (c *Client) WatchHistory(ctx context.Context, cursor HistoryCursor) (*HistoryPage, error) {
	q := url.Values{}
	q.Set("ps", "20")
	q.Set("business", "archive") // user-uploaded videos only; skip bangumi/articles
	if cursor.Max > 0 {
		q.Set("max", strconv.FormatInt(cursor.Max, 10))
	}
	if cursor.ViewAt > 0 {
		q.Set("view_at", strconv.FormatInt(cursor.ViewAt, 10))
	}
	endpoint := "https://api.bilibili.com/x/web-interface/history/cursor?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Cursor struct {
				Max    int64 `json:"max"`
				ViewAt int64 `json:"view_at"`
			} `json:"cursor"`
			List []struct {
				History struct {
					OID      int64  `json:"oid"`
					BVID     string `json:"bvid"`
					CID      int64  `json:"cid"`
					Business string `json:"business"`
				} `json:"history"`
				Title      string `json:"title"`
				Cover      string `json:"cover"`
				ViewAt     int64  `json:"view_at"`
				Progress   int    `json:"progress"`
				Duration   int    `json:"duration"`
				AuthorName string `json:"author_name"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("history: code=%d msg=%q", resp.Code, resp.Msg)
	}

	page := &HistoryPage{
		Next:    HistoryCursor{Max: resp.Data.Cursor.Max, ViewAt: resp.Data.Cursor.ViewAt},
		HasMore: resp.Data.Cursor.Max != 0,
	}
	for _, h := range resp.Data.List {
		if h.History.Business != "archive" || h.History.BVID == "" {
			continue
		}
		page.Items = append(page.Items, HistoryItem{
			BVID:     h.History.BVID,
			AID:      h.History.OID,
			CID:      h.History.CID,
			Title:    h.Title,
			Author:   h.AuthorName,
			ThumbURL: normalizeURL(h.Cover),
			Duration: h.Duration,
			Progress: h.Progress,
			ViewedAt: time.Unix(h.ViewAt, 0),
		})
	}
	return page, nil
}

// VideoProgress returns the user's last-watched position (in seconds) for a
// specific video. Returns 0 if no progress is recorded or the call fails —
// callers should treat this as "no resume needed" rather than an error.
//
// Bilibili's `last_play_time` field is in milliseconds.
func (c *Client) VideoProgress(ctx context.Context, aid, cid int64) (int, error) {
	q := url.Values{}
	q.Set("aid", strconv.FormatInt(aid, 10))
	q.Set("cid", strconv.FormatInt(cid, 10))
	endpoint := "https://api.bilibili.com/x/player/v2?" + q.Encode()

	var resp struct {
		Code int `json:"code"`
		Data struct {
			LastPlayTime int `json:"last_play_time"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return 0, err
	}
	if resp.Code != 0 {
		return 0, nil
	}
	return resp.Data.LastPlayTime / 1000, nil
}

// ReportProgress sends a heartbeat to bilibili so the server-side watch
// position stays current across all clients (this app, bilibili.com,
// the bilibili iOS/Android apps).
func (c *Client) ReportProgress(ctx context.Context, aid, cid int64, bvid string, playedTime int) error {
	csrf := c.cfg.GetCookies().BiliJct
	if csrf == "" {
		return fmt.Errorf("missing bili_jct cookie; cannot send heartbeat")
	}

	body := url.Values{}
	body.Set("aid", strconv.FormatInt(aid, 10))
	body.Set("cid", strconv.FormatInt(cid, 10))
	body.Set("bvid", bvid)
	body.Set("type", "3") // 3 = ugc video
	body.Set("sub_type", "0")
	body.Set("dt", "2")
	body.Set("played_time", strconv.Itoa(playedTime))
	body.Set("realtime", strconv.Itoa(playedTime))
	body.Set("play_type", "1")
	body.Set("csrf", csrf)

	endpoint := "https://api.bilibili.com/x/click-interface/web/heartbeat"
	req, err := c.NewRequest(ctx, "POST", endpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&r); err != nil {
		return err
	}
	if r.Code != 0 {
		return fmt.Errorf("heartbeat: code=%d msg=%q", r.Code, r.Msg)
	}
	return nil
}
