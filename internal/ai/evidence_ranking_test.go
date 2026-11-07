package ai

import "testing"

func TestEvidencePolicyRankingValidation(t *testing.T) {
	for _, ok := range []EvidencePolicy{{}, {EvidenceLimits: EvidenceLimits{RankingProfile: "balanced", SearchWindowDays: 30}}, {EvidenceLimits: EvidenceLimits{RankingProfile: "sentiment_first"}}} {
		if err := ok.Validate(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, bad := range []EvidencePolicy{{EvidenceLimits: EvidenceLimits{RankingProfile: "popular"}}, {EvidenceLimits: EvidenceLimits{SearchWindowDays: -1}}, {EvidenceLimits: EvidenceLimits{SearchWindowDays: MaxEvidenceAgeDays + 1}}} {
		if bad.Validate() == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
}
