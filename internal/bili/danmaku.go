package bili

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// DanmakuItem is one bullet comment overlaid on the video.
//
// Bilibili's `p` attribute (comma-separated) packs:
//
//	p[0] = playback time (seconds, float)
//	p[1] = type: 1=scroll, 4=bottom, 5=top, others=positioned (skipped)
//	p[2] = font size
//	p[3] = color (decimal RGB)
//	p[4] = post timestamp
//	p[5] = pool
//	p[6] = sender hash
//	p[7] = rowid
type DanmakuItem struct {
	TimeSec float64 `json:"t"`
	Type    int     `json:"k"` // 1 scroll, 4 bottom, 5 top
	Color   uint32  `json:"c"`
	Text    string  `json:"x"`
}

// FetchDanmaku downloads the public XML danmaku list for a cid and parses it
// into a time-sorted slice. Uses the comment.bilibili.com endpoint which
// returns plain (uncompressed) XML.
func (c *Client) FetchDanmaku(ctx context.Context, cid int64) ([]DanmakuItem, error) {
	url := fmt.Sprintf("https://comment.bilibili.com/%d.xml", cid)
	req, err := c.NewRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("danmaku xml: status %d body=%q", resp.StatusCode, b)
	}

	// Bilibili returns danmaku XML compressed as raw deflate (RFC 1951, no
	// zlib wrapper). Some endpoints occasionally wrap with zlib instead, so we
	// try raw deflate first and fall back to zlib if that fails.
	var bodyReader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "deflate" {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		decompressed, ferr := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
		if ferr != nil {
			zr, zerr := zlib.NewReader(bytes.NewReader(raw))
			if zerr != nil {
				return nil, fmt.Errorf("decompress deflate: %v / zlib: %v", ferr, zerr)
			}
			decompressed, err = io.ReadAll(zr)
			zr.Close()
			if err != nil {
				return nil, err
			}
		}
		bodyReader = bytes.NewReader(decompressed)
	}

	var doc struct {
		XMLName xml.Name `xml:"i"`
		D       []struct {
			P    string `xml:"p,attr"`
			Text string `xml:",chardata"`
		} `xml:"d"`
	}
	dec := xml.NewDecoder(bodyReader)
	// Some bilibili responses claim "GBK" encoding but are actually UTF-8.
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}

	out := make([]DanmakuItem, 0, len(doc.D))
	for _, d := range doc.D {
		parts := strings.Split(d.P, ",")
		if len(parts) < 4 {
			continue
		}
		typ, _ := strconv.Atoi(parts[1])
		if typ != 1 && typ != 4 && typ != 5 {
			continue
		}
		t, _ := strconv.ParseFloat(parts[0], 64)
		col, _ := strconv.ParseUint(parts[3], 10, 32)
		out = append(out, DanmakuItem{
			TimeSec: t,
			Type:    typ,
			Color:   uint32(col),
			Text:    d.Text,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimeSec < out[j].TimeSec })
	return out, nil
}
