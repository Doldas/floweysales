package redact

import (
	"strings"
	"testing"
)

func TestStringMasksCredentialFormats(t *testing.T) {
	cases := map[string]string{
		"Authorization: Bearer abcdefghijklmnop12345":                      "Authorization: " + Mask,
		"header bearer abc.def.ghi-jklmnop":                                "header bearer " + Mask,
		"key sk-proj-ABCDEFGHIJKLMNOPQRSTUV in text":                       "key " + Mask + " in text",
		"token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 end":                     "token " + Mask + " end",
		"aws AKIAABCDEFGHIJKLMNOP done":                                    "aws " + Mask + " done",
		"slack xoxb-1234567890-abcdef":                                     "slack " + Mask,
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijk": "jwt " + Mask,
		"https://api.example.com/v1?symbol=AAPL&apikey=SECRET123&x=1":      "https://api.example.com/v1?symbol=AAPL&apikey=" + Mask + "&x=1",
		`{"api_key": "abcd1234efgh", "symbol": "AAPL"}`:                    `{"api_key": "` + Mask + `", "symbol": "AAPL"}`,
		"password=hunter2hunter2 next":                                     "password=" + Mask + " next",
	}
	for in, want := range cases {
		if got := String(in); got != want {
			t.Errorf("String(%q)\n got %q\nwant %q", in, got, want)
		}
	}
	pem := "before -----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY----- after"
	if got := String(pem); got != "before "+Mask+" after" {
		t.Errorf("private key block not masked: %q", got)
	}
}

func TestStringKeepsOrdinaryResearchText(t *testing.T) {
	for _, s := range []string{
		"NVDA revenue grew 94% year over year; the key risk is supply.",
		`{"symbol":"AAPL","price":191.2,"pe_ratio":29.1}`,
		"Token economics of the protocol are discussed in section 4.",
		"The password policy section of the 10-K is short.",
	} {
		if got := String(s); got != s {
			t.Errorf("ordinary text changed:\n in %q\nout %q", s, got)
		}
	}
}

func TestRegisteredSecretsAreMaskedExactly(t *testing.T) {
	RegisterSecret("configured-finnhub-key-123")
	RegisterSecret("short")
	got := String("call with configured-finnhub-key-123 and short words")
	if strings.Contains(got, "configured-finnhub-key-123") || !strings.Contains(got, "short words") {
		t.Fatalf("registered secret handling: %q", got)
	}
}

func TestPreviewRedactsBeforeTruncatingAndKeepsUTF8(t *testing.T) {
	s := strings.Repeat("a", 10) + " sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if got := Preview(s, 20); strings.Contains(got, "sk-ABC") {
		t.Fatalf("secret cut by the limit leaked: %q", got)
	}
	if got := Truncate("åäö", 3); got != "å" {
		t.Fatalf("UTF-8 split: %q", got)
	}
}
