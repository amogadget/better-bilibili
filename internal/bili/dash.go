package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// DASHStream describes the audio + video URLs that ffmpeg can read directly.
// Bilibili returns separate fragmented-mp4 streams for video and audio, each
// served from a signed CDN URL that expires within hours.
type DASHStream struct {
	VideoURL string
	AudioURL string
	Quality  int
	Codec    string // e.g. "avc1.640020" or "hev1.1.6.L153.90"
	WidthPx  int
	HeightPx int
}

// PlayURLDASH resolves the DASH (audio+video split) version of a video. qn
// picks the desired video quality (32=480, 64=720, 80=1080).
func (c *Client) PlayURLDASH(ctx context.Context, bvid string, cid int64, qn int) (*DASHStream, error) {
	q := url.Values{}
	q.Set("bvid", bvid)
	q.Set("cid", strconv.FormatInt(cid, 10))
	q.Set("qn", strconv.Itoa(qn))
	q.Set("fnval", "16") // DASH
	q.Set("fnver", "0")
	q.Set("fourk", "0")
	endpoint := "https://api.bilibili.com/x/player/playurl?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Dash struct {
				Video []dashTrack `json:"video"`
				Audio []dashTrack `json:"audio"`
			} `json:"dash"`
		} `json:"data"`
	}
	if err := c.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("playurl dash: code=%d msg=%q", resp.Code, resp.Msg)
	}
	if len(resp.Data.Dash.Video) == 0 || len(resp.Data.Dash.Audio) == 0 {
		return nil, fmt.Errorf("playurl dash: empty tracks")
	}

	// Pick the video track closest to (but not exceeding) the requested qn.
	video := resp.Data.Dash.Video[0]
	for _, v := range resp.Data.Dash.Video {
		if v.ID <= qn && v.ID > video.ID {
			video = v
		}
	}
	// Audio: highest bandwidth.
	audio := resp.Data.Dash.Audio[0]
	for _, a := range resp.Data.Dash.Audio {
		if a.Bandwidth > audio.Bandwidth {
			audio = a
		}
	}

	return &DASHStream{
		VideoURL: video.BaseURL,
		AudioURL: audio.BaseURL,
		Quality:  video.ID,
		Codec:    video.Codecs,
		WidthPx:  video.Width,
		HeightPx: video.Height,
	}, nil
}

type dashTrack struct {
	ID        int    `json:"id"`
	BaseURL   string `json:"baseUrl"`
	Bandwidth int    `json:"bandwidth"`
	Codecs    string `json:"codecs"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}
