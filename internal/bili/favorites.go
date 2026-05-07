package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type FavFolder struct {
	ID    int64
	Title string
	Count int
}

type FavMedia struct {
	BVID     string
	Title    string
	ThumbURL string
	Duration int // seconds
	Owner    string
}

// FavoriteFolders lists the folders created by the user identified by mid.
func (c *Client) FavoriteFolders(ctx context.Context, mid string) ([]FavFolder, error) {
	if mid == "" {
		return nil, fmt.Errorf("favorite folders: missing user mid")
	}
	endpoint := "https://api.bilibili.com/x/v3/fav/folder/created/list-all?up_mid=" + url.QueryEscape(mid)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			List []struct {
				ID         int64  `json:"id"`
				Title      string `json:"title"`
				MediaCount int    `json:"media_count"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("fav folders: code=%d msg=%q", resp.Code, resp.Msg)
	}
	out := make([]FavFolder, 0, len(resp.Data.List))
	for _, f := range resp.Data.List {
		out = append(out, FavFolder{ID: f.ID, Title: f.Title, Count: f.MediaCount})
	}
	return out, nil
}

type FavMediaPage struct {
	Items   []FavMedia
	HasMore bool
}

// FavoriteMedia lists videos inside a folder. page starts at 1.
func (c *Client) FavoriteMedia(ctx context.Context, fid int64, page int) (*FavMediaPage, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("media_id", strconv.FormatInt(fid, 10))
	q.Set("pn", strconv.Itoa(page))
	q.Set("ps", "20")
	q.Set("platform", "web")
	endpoint := "https://api.bilibili.com/x/v3/fav/resource/list?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			HasMore bool `json:"has_more"`
			Medias  []struct {
				BVID     string `json:"bvid"`
				Title    string `json:"title"`
				Cover    string `json:"cover"`
				Duration int    `json:"duration"`
				Upper    struct {
					Name string `json:"name"`
				} `json:"upper"`
			} `json:"medias"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("fav resource list: code=%d msg=%q", resp.Code, resp.Msg)
	}
	out := &FavMediaPage{HasMore: resp.Data.HasMore}
	for _, m := range resp.Data.Medias {
		if m.BVID == "" {
			continue
		}
		out.Items = append(out.Items, FavMedia{
			BVID:     m.BVID,
			Title:    m.Title,
			ThumbURL: normalizeURL(m.Cover),
			Duration: m.Duration,
			Owner:    m.Upper.Name,
		})
	}
	return out, nil
}
