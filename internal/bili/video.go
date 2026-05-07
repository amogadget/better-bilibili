package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// VideoInfo is a minimal subset of x/web-interface/view useful for rendering a
// watch page (title, uploader, default cid, list of pages for multi-part videos).
type VideoInfo struct {
	BVID        string
	AID         int64
	CID         int64 // first page's cid; for multi-part videos see Pages
	Title       string
	Description string
	Owner       string
	OwnerID     int64
	Thumb       string
	Duration    int // seconds
	Pages       []VideoPage
	Season      *Season // populated when this video is part of a creator's UGC series
}

type VideoPage struct {
	CID      int64
	Page     int
	Title    string
	Duration int // seconds
}

type Season struct {
	ID       int64
	Title    string
	Episodes []SeasonEpisode
}

type SeasonEpisode struct {
	BVID     string
	Title    string
	ThumbURL string
	Duration string // formatted "mm:ss"
}

func (c *Client) VideoInfo(ctx context.Context, bvid string) (*VideoInfo, error) {
	endpoint := "https://api.bilibili.com/x/web-interface/view?bvid=" + url.QueryEscape(bvid)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			BVID  string `json:"bvid"`
			AID   int64  `json:"aid"`
			CID   int64  `json:"cid"`
			Title string `json:"title"`
			Desc  string `json:"desc"`
			Pic   string `json:"pic"`
			Dur   int    `json:"duration"`
			Owner struct {
				Name string `json:"name"`
				MID  int64  `json:"mid"`
			} `json:"owner"`
			Pages []struct {
				CID      int64  `json:"cid"`
				Page     int    `json:"page"`
				Part     string `json:"part"`
				Duration int    `json:"duration"`
			} `json:"pages"`
			UGCSeason struct {
				ID       int64  `json:"id"`
				Title    string `json:"title"`
				Sections []struct {
					Episodes []struct {
						BVID  string `json:"bvid"`
						Title string `json:"title"`
						Arc   struct {
							Duration int    `json:"duration"`
							Pic      string `json:"pic"`
						} `json:"arc"`
					} `json:"episodes"`
				} `json:"sections"`
			} `json:"ugc_season"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("view: code=%d msg=%q", resp.Code, resp.Msg)
	}
	v := &VideoInfo{
		BVID:        resp.Data.BVID,
		AID:         resp.Data.AID,
		CID:         resp.Data.CID,
		Title:       resp.Data.Title,
		Description: resp.Data.Desc,
		Owner:       resp.Data.Owner.Name,
		OwnerID:     resp.Data.Owner.MID,
		Thumb:       normalizeURL(resp.Data.Pic),
		Duration:    resp.Data.Dur,
	}
	for _, p := range resp.Data.Pages {
		v.Pages = append(v.Pages, VideoPage{
			CID: p.CID, Page: p.Page, Title: p.Part, Duration: p.Duration,
		})
	}
	if resp.Data.UGCSeason.ID != 0 {
		s := &Season{
			ID:    resp.Data.UGCSeason.ID,
			Title: resp.Data.UGCSeason.Title,
		}
		for _, sec := range resp.Data.UGCSeason.Sections {
			for _, ep := range sec.Episodes {
				if ep.BVID == "" {
					continue
				}
				s.Episodes = append(s.Episodes, SeasonEpisode{
					BVID:     ep.BVID,
					Title:    ep.Title,
					ThumbURL: normalizeURL(ep.Arc.Pic),
					Duration: FormatDuration(ep.Arc.Duration),
				})
			}
		}
		if len(s.Episodes) > 0 {
			v.Season = s
		}
	}
	return v, nil
}

// MP4Stream describes the upstream mp4 source resolved from playurl.
type MP4Stream struct {
	URL    string // signed CDN URL; expires within hours
	Size   int64
	LengthMs int64 // duration in ms reported by bilibili
	Quality int
	// Parts > 1 means the legacy fnval=1 endpoint split the video; v1 only plays the first.
	Parts int
}

// PlayURLMP4 resolves a single-file mp4 URL via the legacy fnval=1 endpoint.
// qn is the quality code (32=480, 64=720, 80=1080).
// Returns ErrMultipart if bilibili splits the video into multiple parts (for now we still return the first).
func (c *Client) PlayURLMP4(ctx context.Context, bvid string, cid int64, qn int) (*MP4Stream, error) {
	q := url.Values{}
	q.Set("bvid", bvid)
	q.Set("cid", strconv.FormatInt(cid, 10))
	q.Set("qn", strconv.Itoa(qn))
	q.Set("fnval", "1")
	q.Set("fnver", "0")
	q.Set("fourk", "0")
	q.Set("platform", "html5")
	q.Set("high_quality", "1")
	endpoint := "https://api.bilibili.com/x/player/playurl?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Quality int `json:"quality"`
			Durl    []struct {
				URL    string `json:"url"`
				Size   int64  `json:"size"`
				Length int64  `json:"length"`
			} `json:"durl"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("playurl: code=%d msg=%q", resp.Code, resp.Msg)
	}
	if len(resp.Data.Durl) == 0 {
		return nil, fmt.Errorf("playurl: empty durl")
	}
	first := resp.Data.Durl[0]
	return &MP4Stream{
		URL:      first.URL,
		Size:     first.Size,
		LengthMs: first.Length,
		Quality:  resp.Data.Quality,
		Parts:    len(resp.Data.Durl),
	}, nil
}
