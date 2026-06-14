package dailymed

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes DailyMed as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/dailymed-cli/dailymed"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// dailymed:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone dailymed binary, so the binary and a host
// share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the DailyMed driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "dailymed",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "dailymed",
			Short:  "Fetch FDA drug label (SPL) data from DailyMed",
			Long: `Fetch FDA drug label (SPL) data from DailyMed

dailymed reads public DailyMed data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools. No API
key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/dailymed-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// search: find drug labels by name.
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		Summary: "Search drug labels by name",
		Args:    []kit.Arg{{Name: "query", Help: "drug name to search for"}},
	}, searchSPLs)

	// spl: get a single SPL by setid.
	kit.Handle(app, kit.OpMeta{
		Name:    "spl",
		Group:   "read",
		Single:  true,
		Summary: "Get a drug label (SPL) by setid",
		URIType: "spl",
		Resolver: true,
		Args:    []kit.Arg{{Name: "setid", Help: "SPL setid (UUID)"}},
	}, getSPL)

	// ndcs: list NDC codes for a drug label.
	kit.Handle(app, kit.OpMeta{
		Name:    "ndcs",
		Group:   "read",
		List:    true,
		Summary: "List NDC codes for a drug label",
		URIType: "spl",
		Args:    []kit.Arg{{Name: "setid", Help: "SPL setid (UUID)"}},
	}, listNDCs)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
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
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type searchIn struct {
	Query  string  `kit:"arg" help:"drug name to search for"`
	Limit  int     `kit:"flag,inherit" help:"max results" default:"20"`
	Page   int     `kit:"flag" help:"page number (1-based)" default:"1"`
	Client *Client `kit:"inject"`
}

type splIn struct {
	SetID  string  `kit:"arg" help:"SPL setid (UUID)"`
	Client *Client `kit:"inject"`
}

type ndcsIn struct {
	SetID  string  `kit:"arg" help:"SPL setid (UUID)"`
	Limit  int     `kit:"flag,inherit" help:"max results" default:"20"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func searchSPLs(ctx context.Context, in searchIn, emit func(SPL) error) error {
	spls, err := in.Client.SearchSPLs(ctx, in.Query, in.Limit, in.Page)
	if err != nil {
		return mapErr(err)
	}
	for _, s := range spls {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

func getSPL(ctx context.Context, in splIn, emit func(*SPL) error) error {
	s, err := in.Client.GetSPL(ctx, in.SetID)
	if err != nil {
		return mapErr(err)
	}
	return emit(s)
}

func listNDCs(ctx context.Context, in ndcsIn, emit func(NDC) error) error {
	ndcs, err := in.Client.ListNDCs(ctx, in.SetID, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, n := range ndcs {
		if err := emit(n); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver ---

// Classify turns a setid or full DailyMed URL into (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	id = extractSetID(input)
	if id == "" {
		return "", "", errs.Usage("unrecognized DailyMed reference: %q", input)
	}
	return "spl", id, nil
}

// Locate is the inverse: the live page URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "spl" {
		return "", errs.Usage("dailymed has no resource type %q", uriType)
	}
	return "https://" + Host + "/dailymed/drugInfo.cfm?setid=" + id, nil
}

// --- helpers ---

// extractSetID returns the setid from a full DailyMed URL or the bare input.
func extractSetID(input string) string {
	input = trimSpace(input)
	if input == "" {
		return ""
	}
	// Accept a bare setid directly (UUID form or any non-empty string).
	return input
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

func mapErr(err error) error {
	return err
}
