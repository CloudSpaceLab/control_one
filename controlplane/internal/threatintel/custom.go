package threatintel

import (
	"context"
	"net/http"
)

// CustomLineList accepts any URL that returns one IP or CIDR per line.
// Operators use this for FireHOL Level 2/3, internal honeypot dumps, etc.
type CustomLineList struct {
	URL      string
	FeedName string
	Category string
	Score    int
}

func (c CustomLineList) Name() string {
	if c.FeedName != "" {
		return c.FeedName
	}
	return "custom-lines"
}

func (c CustomLineList) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	body, err := fetchBody(ctx, client, c.URL)
	if err != nil {
		return nil, err
	}
	score := c.Score
	if score <= 0 {
		score = 70
	}
	cat := c.Category
	if cat == "" {
		cat = "custom"
	}
	return parseLineList(body, c.Name(), cat, score), nil
}

// CustomSpamhausFormat accepts URLs that match the Spamhaus DROP wire format
// ("<cidr> ; <evidence>"). Common for shared IT-team blocklists.
type CustomSpamhausFormat struct {
	URL      string
	FeedName string
}

func (c CustomSpamhausFormat) Name() string {
	if c.FeedName != "" {
		return c.FeedName
	}
	return "custom-spamhaus"
}

func (c CustomSpamhausFormat) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	body, err := fetchBody(ctx, client, c.URL)
	if err != nil {
		return nil, err
	}
	return parseSpamhaus(body, c.Name()), nil
}
