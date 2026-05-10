package server

import (
	"fmt"
	"time"

	"bili-web-vps/internal/bili"
)

// tplBase carries fields the shared topbar partial needs on every page.
type tplBase struct {
	Section string // active subnav: "home", "subscriptions", "favorites", "watch-later"
	Query   string // current search query, populated into the search input
}

// NextLink, when non-empty, drives a "Load more" anchor at the bottom of any
// list page.
type NextLink struct {
	URL   string
	Label string // optional override; defaults to "Load more"
}

// CardView is the unified shape rendered by the {{template "card"}} partial.
type CardView struct {
	BVID     string
	Title    string
	ThumbURL string
	Duration string
	Author   string
	Subtitle string
}

func cardFromRecommend(r bili.RecommendItem) CardView {
	subtitle := ""
	if r.Views > 0 {
		subtitle = bili.FormatCount(r.Views) + " views"
	}
	return CardView{
		BVID:     r.BVID,
		Title:    r.Title,
		ThumbURL: r.ThumbURL,
		Duration: bili.FormatDuration(r.Duration),
		Author:   r.Author,
		Subtitle: subtitle,
	}
}

func cardFromFeed(f bili.FeedItem) CardView {
	return CardView{
		BVID:     f.BVID,
		Title:    f.Title,
		ThumbURL: f.ThumbURL,
		Duration: f.Duration,
		Author:   f.Author,
		Subtitle: humanTime(f.Published),
	}
}

func cardFromFavMedia(m bili.FavMedia) CardView {
	return CardView{
		BVID:     m.BVID,
		Title:    m.Title,
		ThumbURL: m.ThumbURL,
		Duration: bili.FormatDuration(m.Duration),
		Author:   m.Owner,
	}
}

func cardFromWatchLater(w bili.WatchLaterItem) CardView {
	return CardView{
		BVID:     w.BVID,
		Title:    w.Title,
		ThumbURL: w.ThumbURL,
		Duration: bili.FormatDuration(w.Duration),
		Author:   w.Owner,
	}
}

func cardFromHistory(h bili.HistoryItem) CardView {
	return CardView{
		BVID:     h.BVID,
		Title:    h.Title,
		ThumbURL: h.ThumbURL,
		Duration: bili.FormatDuration(h.Duration),
		Author:   h.Author,
		Subtitle: humanTime(h.ViewedAt),
	}
}

type CommentView struct {
	Author      string
	AvatarURL   string
	Text        string
	Likes       int64
	Replies     int
	PostedHuman string
}

func commentToView(c bili.Comment) CommentView {
	return CommentView{
		Author:      c.Author,
		AvatarURL:   c.AvatarURL,
		Text:        c.Text,
		Likes:       c.Likes,
		Replies:     c.Replies,
		PostedHuman: humanTime(c.Posted),
	}
}

func cardFromSearch(s bili.SearchResult) CardView {
	subtitle := ""
	if s.PlayCount > 0 {
		subtitle = bili.FormatCount(s.PlayCount) + " views"
	}
	return CardView{
		BVID:     s.BVID,
		Title:    s.Title,
		ThumbURL: s.ThumbURL,
		Duration: s.Duration,
		Author:   s.Author,
		Subtitle: subtitle,
	}
}

func humanTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
