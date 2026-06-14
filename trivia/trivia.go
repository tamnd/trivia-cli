// Package trivia is the library behind the trivia command line:
// the HTTP client, request shaping, and typed data models for the-trivia-api.com.
//
// The Client sets a real User-Agent, paces requests, and retries transient
// failures (429 and 5xx) with exponential backoff. Two operations are provided:
// get random trivia questions (Questions) and list all categories (Categories).
package trivia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Host is the site this client talks to.
const Host = "the-trivia-api.com"

// Config holds tunable knobs for the HTTP client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns sensible defaults for production use.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://the-trivia-api.com/v2",
		UserAgent: "trivia-cli/0.1.0 (github.com/tamnd/trivia-cli)",
		Rate:      200 * time.Millisecond,
		Timeout:   30 * time.Second,
		Retries:   3,
	}
}

// Client talks to the-trivia-api.com over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client configured with cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// Question is one trivia question with its correct and incorrect answers.
type Question struct {
	ID               string   `json:"id"`
	Category         string   `json:"category"`
	Question         string   `json:"question"`
	CorrectAnswer    string   `json:"correct_answer"`
	IncorrectAnswers []string `json:"incorrect_answers"`
	Difficulty       string   `json:"difficulty"`
	Tags             []string `json:"tags"`
	IsNiche          bool     `json:"is_niche"`
}

// Category groups a category name with its associated tags.
type Category struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// wireQuestion matches the actual JSON shape returned by the API.
type wireQuestion struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Question struct {
		Text string `json:"text"`
	} `json:"question"`
	CorrectAnswer    string   `json:"correctAnswer"`
	IncorrectAnswers []string `json:"incorrectAnswers"`
	Difficulty       string   `json:"difficulty"`
	Tags             []string `json:"tags"`
	IsNiche          bool     `json:"isNiche"`
}

// Questions returns random trivia questions with optional filters.
// limit must be > 0; category and difficulty may be empty strings to omit the filter.
func (c *Client) Questions(ctx context.Context, limit int, category, difficulty string) ([]Question, error) {
	rawURL := c.cfg.BaseURL + "/questions?limit=" + strconv.Itoa(limit)
	if category != "" {
		rawURL += "&categories=" + url.QueryEscape(category)
	}
	if difficulty != "" {
		rawURL += "&difficulty=" + url.QueryEscape(difficulty)
	}

	b, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var wires []wireQuestion
	if err := json.Unmarshal(b, &wires); err != nil {
		return nil, fmt.Errorf("decode questions: %w", err)
	}

	out := make([]Question, 0, len(wires))
	for _, w := range wires {
		out = append(out, Question{
			ID:               w.ID,
			Category:         w.Category,
			Question:         w.Question.Text,
			CorrectAnswer:    w.CorrectAnswer,
			IncorrectAnswers: w.IncorrectAnswers,
			Difficulty:       w.Difficulty,
			Tags:             w.Tags,
			IsNiche:          w.IsNiche,
		})
	}
	return out, nil
}

// Categories returns all available categories with their tags, sorted by name.
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	rawURL := c.cfg.BaseURL + "/categories"
	b, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var m map[string][]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}

	out := make([]Category, 0, len(m))
	for name, tags := range m {
		out = append(out, Category{Name: name, Tags: tags})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// get fetches url and returns the response body. It paces and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
