package dailymed

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring, which need no network. The client's HTTP behaviour is
// covered in dailymed_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "dailymed" {
		t.Errorf("Scheme = %q, want dailymed", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "dailymed" {
		t.Errorf("Identity.Binary = %q, want dailymed", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	const setid = "a0040a07-4cd5-4a80-b7b2-3a5fe75e2e5e"
	cases := []struct{ in, typ, id string }{
		{setid, "spl", setid},
		{"  " + setid + "  ", "spl", setid},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return an error")
	}
}

func TestLocate(t *testing.T) {
	const setid = "a0040a07-4cd5-4a80-b7b2-3a5fe75e2e5e"
	got, err := Domain{}.Locate("spl", setid)
	want := "https://" + Host + "/dailymed/drugInfo.cfm?setid=" + setid
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "foo")
	if err == nil {
		t.Error("Locate with unknown type should return an error")
	}
}

// TestHostWiring mounts the driver in a kit Host and checks that Mint and
// ResolveOn work correctly. The init in domain.go registers the domain.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	const setid = "a0040a07-4cd5-4a80-b7b2-3a5fe75e2e5e"
	s := &SPL{SetID: setid, Title: "Aspirin 81 mg", PublishedDate: "Jun 01, 2026"}
	u, err := h.Mint(s)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	want := "dailymed://spl/" + setid
	if u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("dailymed", setid)
	if err != nil || got.String() != "dailymed://spl/"+setid {
		t.Errorf("ResolveOn = (%q, %v), want dailymed://spl/%s", got.String(), err, setid)
	}
}
