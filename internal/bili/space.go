package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// ChannelInfo is the header data for a creator's space page.
type ChannelInfo struct {
	MID       int64
	Name      string
	AvatarURL string
	Sign      string
	Followers int64
}

// ChannelVideo is one video on a creator's space page.
type ChannelVideo struct {
	BVID     string
	Title    string
	ThumbURL string
	Duration string // bilibili gives "mm:ss" pre-formatted here
	Plays    int64
	Posted   time.Time
}

type ChannelVideosPage struct {
	Items   []ChannelVideo
	Total   int
	HasMore bool
}

// ChannelSeason is one bilibili "合集" (UGC season) — a creator-curated
// ordered list of their own videos.
type ChannelSeason struct {
	SeasonID  int64
	Name      string
	CoverURL  string
	Count     int
	FirstBVID string // best-effort; empty if not in initial response
}

type ChannelSeasonsPage struct {
	Items   []ChannelSeason
	Total   int
	HasMore bool
}

// SeasonVideo is one entry inside a season's archive list.
type SeasonVideo struct {
	BVID     string
	Title    string
	ThumbURL string
	Duration int // seconds (this endpoint uses int, not "mm:ss")
	Plays    int64
}

type SeasonVideosPage struct {
	Items   []SeasonVideo
	Total   int
	HasMore bool
}

// orderForSpace sanitizes the order parameter; bilibili accepts pubdate/click/stow.
func orderForSpace(s string) string {
	switch s {
	case "click", "stow":
		return s
	default:
		return "pubdate"
	}
}

// ChannelInfo fetches the header (name, avatar, sign) plus the follower count.
func (c *Client) ChannelInfo(ctx context.Context, mid int64) (*ChannelInfo, error) {
	q := url.Values{}
	q.Set("mid", strconv.FormatInt(mid, 10))
	if err := c.SignWBI(ctx, q); err != nil {
		return nil, err
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			MID  int64  `json:"mid"`
			Name string `json:"name"`
			Face string `json:"face"`
			Sign string `json:"sign"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "https://api.bilibili.com/x/space/wbi/acc/info?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("space info: code=%d msg=%q", resp.Code, resp.Msg)
	}
	info := &ChannelInfo{
		MID:       resp.Data.MID,
		Name:      resp.Data.Name,
		AvatarURL: normalizeURL(resp.Data.Face),
		Sign:      resp.Data.Sign,
	}

	// Follower count is on a separate endpoint. Best-effort.
	var stat struct {
		Code int `json:"code"`
		Data struct {
			Follower int64 `json:"follower"`
		} `json:"data"`
	}
	statURL := "https://api.bilibili.com/x/relation/stat?vmid=" + strconv.FormatInt(mid, 10)
	if err := c.GetJSON(ctx, statURL, &stat); err == nil && stat.Code == 0 {
		info.Followers = stat.Data.Follower
	}
	return info, nil
}

// ChannelVideos fetches one page of a creator's video uploads.
func (c *Client) ChannelVideos(ctx context.Context, mid int64, page int, order string) (*ChannelVideosPage, error) {
	if page < 1 {
		page = 1
	}
	const ps = 30
	q := url.Values{}
	q.Set("mid", strconv.FormatInt(mid, 10))
	q.Set("ps", strconv.Itoa(ps))
	q.Set("pn", strconv.Itoa(page))
	q.Set("order", orderForSpace(order))
	q.Set("keyword", "")
	if err := c.SignWBI(ctx, q); err != nil {
		return nil, err
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			List struct {
				Vlist []struct {
					BVID    string `json:"bvid"`
					Title   string `json:"title"`
					Pic     string `json:"pic"`
					Length  string `json:"length"`
					Play    int64  `json:"play"`
					Created int64  `json:"created"`
				} `json:"vlist"`
			} `json:"list"`
			Page struct {
				Count int `json:"count"`
			} `json:"page"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "https://api.bilibili.com/x/space/wbi/arc/search?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("space arc/search: code=%d msg=%q", resp.Code, resp.Msg)
	}
	out := &ChannelVideosPage{Total: resp.Data.Page.Count}
	for _, v := range resp.Data.List.Vlist {
		if v.BVID == "" {
			continue
		}
		out.Items = append(out.Items, ChannelVideo{
			BVID:     v.BVID,
			Title:    v.Title,
			ThumbURL: normalizeURL(v.Pic),
			Duration: v.Length,
			Plays:    v.Play,
			Posted:   time.Unix(v.Created, 0),
		})
	}
	out.HasMore = page*ps < resp.Data.Page.Count
	return out, nil
}

// ChannelSeasons fetches one page of a creator's UGC seasons (合集).
func (c *Client) ChannelSeasons(ctx context.Context, mid int64, page int) (*ChannelSeasonsPage, error) {
	if page < 1 {
		page = 1
	}
	const ps = 10
	q := url.Values{}
	q.Set("mid", strconv.FormatInt(mid, 10))
	q.Set("page_num", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(ps))

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			ItemsLists struct {
				Page struct {
					Total int `json:"total"`
				} `json:"page"`
				SeasonsList []struct {
					Meta struct {
						SeasonID int64  `json:"season_id"`
						Name     string `json:"name"`
						Cover    string `json:"cover"`
						Total    int    `json:"total"`
					} `json:"meta"`
					Archives []struct {
						BVID string `json:"bvid"`
					} `json:"archives"`
				} `json:"seasons_list"`
			} `json:"items_lists"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "https://api.bilibili.com/x/polymer/web-space/seasons_series_list?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("seasons list: code=%d msg=%q", resp.Code, resp.Msg)
	}
	out := &ChannelSeasonsPage{Total: resp.Data.ItemsLists.Page.Total}
	for _, s := range resp.Data.ItemsLists.SeasonsList {
		season := ChannelSeason{
			SeasonID: s.Meta.SeasonID,
			Name:     s.Meta.Name,
			CoverURL: normalizeURL(s.Meta.Cover),
			Count:    s.Meta.Total,
		}
		if len(s.Archives) > 0 {
			season.FirstBVID = s.Archives[0].BVID
		}
		out.Items = append(out.Items, season)
	}
	out.HasMore = page*ps < resp.Data.ItemsLists.Page.Total
	return out, nil
}

// SeasonVideos fetches the archives in a single season.
func (c *Client) SeasonVideos(ctx context.Context, mid, seasonID int64, page int) (*SeasonVideosPage, error) {
	if page < 1 {
		page = 1
	}
	const ps = 30
	q := url.Values{}
	q.Set("mid", strconv.FormatInt(mid, 10))
	q.Set("season_id", strconv.FormatInt(seasonID, 10))
	q.Set("page_num", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(ps))

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Page struct {
				Total int `json:"total"`
			} `json:"page"`
			Archives []struct {
				BVID     string `json:"bvid"`
				Title    string `json:"title"`
				Pic      string `json:"pic"`
				Duration int    `json:"duration"`
				Stat     struct {
					View int64 `json:"view"`
				} `json:"stat"`
			} `json:"archives"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, "https://api.bilibili.com/x/polymer/web-space/seasons_archives_list?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("season archives: code=%d msg=%q", resp.Code, resp.Msg)
	}
	out := &SeasonVideosPage{Total: resp.Data.Page.Total}
	for _, a := range resp.Data.Archives {
		if a.BVID == "" {
			continue
		}
		out.Items = append(out.Items, SeasonVideo{
			BVID:     a.BVID,
			Title:    a.Title,
			ThumbURL: normalizeURL(a.Pic),
			Duration: a.Duration,
			Plays:    a.Stat.View,
		})
	}
	out.HasMore = page*ps < resp.Data.Page.Total
	return out, nil
}
