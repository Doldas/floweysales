package sources

import (
	"encoding/json"
	"strings"
	"testing"
)

const delegationResult = `{"team":"Value panel","team_id":3,"failed":0,"note":"n","members":[
 {"persona":"owner","name":"Owner","answer":"Revenue was 4,200 million.","sources":[
  {"id":"S1","tool":"search_web","url":"https://news.example/acme-q3","title":"Acme Q3","class":"web_page","access":"listed","excerpt":"Acme revenue was 4,200 million in Q3."},
  {"id":"S2","tool":"get_sec_section","url":"https://www.sec.gov/Archives/acme-10k.htm","title":"10-K 7","class":"primary_filing","access":"read","document_id":"0000001-26-000001","excerpt":"Net income was 910.5 million."}]},
 {"persona":"quant","name":"Quant","answer":"No sources.","sources":[
  {"id":"S1","tool":"search_web","url":"https://news.example/acme-q3","class":"web_page","access":"listed","excerpt":"Acme revenue was 4,200 million in Q3."},
  {"id":"S2","tool":"search_web","class":"web_page","access":"listed"}]}]}`

func TestSourcesOfDelegatedMembersJoinTheRegistryWithTheirMember(t *testing.T) {
	r := NewRegistry()
	out, added := r.Register("delegate_to_team", "d1", `{}`, delegationResult)
	if len(added) != 3 {
		t.Fatalf("two sources of Owner and one of Quant (a source with no address is skipped): %+v", added)
	}
	if added[0].ID != "S1" || added[0].Member != "Owner" || added[0].Tool != "delegate_to_team" || added[0].CallID != "d1" || added[0].Access != AccessListed {
		t.Errorf("first: %+v", added[0])
	}
	if added[1].Class != ClassPrimaryFiling || added[1].Access != AccessRead || added[1].DocumentID != "0000001-26-000001" || added[1].Excerpt == "" {
		t.Errorf("a filing a member read stays primary and read: %+v", added[1])
	}
	if added[2].ID != "S3" || added[2].Member != "Quant" {
		t.Errorf("each member's sources are registered under that member: %+v", added[2])
	}
	if !json.Valid([]byte(out)) || !strings.Contains(out, `"source_id":"S1"`) || !strings.Contains(out, `"source_id":"S3"`) || strings.Contains(out, "excerpt") {
		t.Errorf("the model gets ids, not the members' raw source objects: %s", out)
	}
	// The parent can cite them, and the check applies to the member's text.
	if f := Audit("Net income was 910.5 million [S2].", r); len(f) != 0 {
		t.Errorf("a figure in a member's filing excerpt passes: %+v", f)
	}
	if got := kinds(Audit("Net income was 999.5 million [S2].", r)); got != FigureNotIn {
		t.Errorf("a figure not in it fails: %q", got)
	}
	if got := kinds(Audit("Revenue was 4,200 million [S1].", r)); !strings.Contains(got, SnippetOnly) {
		t.Errorf("a member's search hit is only a snippet: %q", got)
	}
	if got := kinds(Audit("Revenue was 4,200 million [S9].", r)); got != Fabricated {
		t.Errorf("an id that was never registered is fabricated: %q", got)
	}
}

func TestDelegationWithoutSourcesPassesThrough(t *testing.T) {
	r := NewRegistry()
	out, added := r.Register("delegate_to_team", "d1", `{}`, `{"members":[{"name":"A","answer":"x"}]}`)
	if len(added) != 0 || !json.Valid([]byte(out)) {
		t.Fatalf("%s %+v", out, added)
	}
	if _, added := r.Register("delegate_to_team", "d1", `{}`, `not json`); added != nil {
		t.Fatal("a result that is not JSON registers nothing")
	}
}
