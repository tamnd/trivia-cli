package trivia_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/trivia-cli/trivia"
)

func newTestClient(ts *httptest.Server) *trivia.Client {
	cfg := trivia.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	return trivia.NewClient(cfg)
}

const mockQuestionsResponse = `[
  {
    "id": "622a1c357cc59eab6f950509",
    "category": "history",
    "correctAnswer": "1492",
    "incorrectAnswers": ["1776", "1066", "1215"],
    "question": {"text": "In what year did Christopher Columbus first reach the Bahamas?"},
    "tags": ["exploration", "americas"],
    "type": "text_choice",
    "difficulty": "easy",
    "isNiche": false
  }
]`

const mockCategoriesResponse = `{
  "Arts & Literature": ["art", "literature", "books", "poetry"],
  "Film & TV": ["movies", "television", "anime", "cartoons"],
  "Food & Drink": ["food", "drink", "wine", "beer"],
  "General Knowledge": ["general_knowledge"],
  "Geography": ["geography", "capitals", "countries", "flags"],
  "History": ["history", "exploration", "wars", "politics"],
  "Music": ["music", "pop", "rock", "jazz", "classical"],
  "Science": ["science", "biology", "chemistry", "physics", "astronomy"],
  "Society & Culture": ["society", "culture", "religion", "mythology"],
  "Sport & Leisure": ["sport", "football", "tennis", "olympics", "games"]
}`

// TestQuestionsSendsUserAgent asserts every request carries a User-Agent header.
func TestQuestionsSendsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			t.Error("request carried no User-Agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockQuestionsResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Questions(context.Background(), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
}

// TestQuestionsParseFields verifies question text, correct answer, and category.
func TestQuestionsParseFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockQuestionsResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	qs, err := c.Questions(context.Background(), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 {
		t.Fatalf("len(qs) = %d, want 1", len(qs))
	}
	q := qs[0]
	wantText := "In what year did Christopher Columbus first reach the Bahamas?"
	if q.Question != wantText {
		t.Errorf("Question = %q, want %q", q.Question, wantText)
	}
	if q.CorrectAnswer != "1492" {
		t.Errorf("CorrectAnswer = %q, want %q", q.CorrectAnswer, "1492")
	}
	if q.Category != "history" {
		t.Errorf("Category = %q, want %q", q.Category, "history")
	}
	if q.Difficulty != "easy" {
		t.Errorf("Difficulty = %q, want %q", q.Difficulty, "easy")
	}
}

// TestCategoriesReturns10 verifies that all 10 known categories are returned.
func TestCategoriesReturns10(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockCategoriesResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	cats, err := c.Categories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 10 {
		t.Errorf("len(cats) = %d, want 10", len(cats))
	}
	// Verify Science is present
	found := false
	for _, cat := range cats {
		if cat.Name == "Science" {
			found = true
			break
		}
	}
	if !found {
		t.Error("category 'Science' not found in results")
	}
}

// TestQuestionsLimitParam verifies the limit is passed as a query parameter.
func TestQuestionsLimitParam(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockQuestionsResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Questions(context.Background(), 5, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "limit=5") {
		t.Errorf("URL = %q, want to contain limit=5", gotURL)
	}
}

// TestQuestionsDifficultyParam verifies the difficulty filter is in the URL.
func TestQuestionsDifficultyParam(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockQuestionsResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Questions(context.Background(), 1, "", "easy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "difficulty=easy") {
		t.Errorf("URL = %q, want to contain difficulty=easy", gotURL)
	}
}

// TestQuestionsRetriesOn503 verifies that the client retries on 5xx responses.
func TestQuestionsRetriesOn503(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockQuestionsResponse)
	}))
	defer srv.Close()

	cfg := trivia.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := trivia.NewClient(cfg)

	start := time.Now()
	qs, err := c.Questions(context.Background(), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) == 0 {
		t.Error("expected questions after retries, got none")
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}
