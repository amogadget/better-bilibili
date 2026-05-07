package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"bili-web-vps/internal/config"
)

// QR login status codes returned by bilibili in data.code:
//
//	0     -> success (cookies are set on the response)
//	86038 -> QR expired
//	86090 -> scanned, awaiting confirmation in app
//	86101 -> not scanned yet
const (
	QRStatusSuccess     = 0
	QRStatusExpired     = 86038
	QRStatusScanned     = 86090
	QRStatusUnscanned   = 86101
)

type QRGenerated struct {
	URL string // url that should be encoded into the QR code image
	Key string // qrcode_key for polling
}

type QRStatus struct {
	Code    int             // bilibili's data.code
	Message string          // human-readable message
	Cookies *config.Cookies // populated only when Code == QRStatusSuccess
}

func (c *Client) QRGenerate(ctx context.Context) (*QRGenerated, error) {
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	err := c.GetJSON(ctx, "https://passport.bilibili.com/x/passport-login/web/qrcode/generate", &resp)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("qrcode/generate: code=%d msg=%q", resp.Code, resp.Msg)
	}
	return &QRGenerated{URL: resp.Data.URL, Key: resp.Data.QRCodeKey}, nil
}

func (c *Client) QRPoll(ctx context.Context, key string) (*QRStatus, error) {
	pollURL := "https://passport.bilibili.com/x/passport-login/web/qrcode/poll?qrcode_key=" + url.QueryEscape(key)
	req, err := c.NewRequest(ctx, "GET", pollURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body struct {
		Code int `json:"code"`
		Data struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return nil, err
	}

	status := &QRStatus{Code: body.Data.Code, Message: body.Data.Message}
	if body.Data.Code == QRStatusSuccess {
		ck := config.Cookies{}
		for _, c := range resp.Cookies() {
			switch c.Name {
			case "SESSDATA":
				ck.SESSDATA = c.Value
			case "bili_jct":
				ck.BiliJct = c.Value
			case "DedeUserID":
				ck.DedeUserID = c.Value
			}
		}
		if ck.SESSDATA == "" {
			return nil, fmt.Errorf("qr poll succeeded but no SESSDATA cookie in response")
		}
		status.Cookies = &ck
	}
	return status, nil
}

// decodeJSON wraps json.NewDecoder with a small status-code guard so callers don't repeat themselves.
func decodeJSON(resp *http.Response, dst any) error {
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d body=%q", resp.StatusCode, b)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
