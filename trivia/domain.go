// domain.go exposes trivia as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/trivia-cli/trivia"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// trivia:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone trivia binary (see cmd/trivia), so the
// binary and a host share one source of truth.
package trivia

import (
	"context"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the trivia driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "trivia",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "trivia",
			Short:  "A command line for The Trivia API.",
			Long: `trivia fetches random trivia questions from the-trivia-api.com.

No API key required. Filter by category (science, history, music ...) and
difficulty (easy, medium, hard). Questions come with the correct answer and
three incorrect answers so you can build your own quiz logic on top.`,
			Site: Host,
			Repo: "https://github.com/tamnd/trivia-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "questions", Group: "read", List: true,
		Summary: "Get random trivia questions",
	}, getQuestions)

	kit.Handle(app, kit.OpMeta{
		Name: "categories", Group: "read", List: true,
		Summary: "List all available categories and their tags",
	}, listCategories)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClient(c), nil
}

// --- inputs ---

type questionsInput struct {
	Limit      int     `kit:"flag" help:"number of questions"`
	Category   string  `kit:"flag" help:"category slug (e.g. science, history)"`
	Difficulty string  `kit:"flag" help:"difficulty: easy, medium, or hard"`
	Client     *Client `kit:"inject"`
}

type categoriesInput struct {
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getQuestions(ctx context.Context, in questionsInput, emit func(*Question) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	qs, err := in.Client.Questions(ctx, limit, in.Category, in.Difficulty)
	if err != nil {
		return errs.NotFound("%s", err.Error())
	}
	for i := range qs {
		if err := emit(&qs[i]); err != nil {
			return err
		}
	}
	return nil
}

func listCategories(ctx context.Context, in categoriesInput, emit func(*Category) error) error {
	cats, err := in.Client.Categories(ctx)
	if err != nil {
		return errs.NotFound("%s", err.Error())
	}
	for i := range cats {
		if err := emit(&cats[i]); err != nil {
			return err
		}
	}
	return nil
}
