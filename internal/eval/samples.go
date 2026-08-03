package eval

import (
	"encoding/json"
	"os"
)

// Sample is one baseline alert/diagnosis case from baseline_samples.json.
// AlertPayload is generic (Aliyun metrics vary); ExpectedKeywords +
// ExpectedRecommendationType tell the judge what good looks like.
type Sample struct {
	ID                         string         `json:"id"`
	Category                   string         `json:"category"`
	Difficulty                 string         `json:"difficulty"`
	AlertPayload               map[string]any `json:"alert_payload"`
	ExpectedKeywords           []string       `json:"expected_root_cause_keywords"`
	ExpectedRecommendationType string         `json:"expected_recommendation_type"`
	ScoringNotes               string         `json:"scoring_notes"`
}

// baselineFile wraps the JSON document; matches Python shape ({"samples": [...]}).
type baselineFile struct {
	Samples []Sample `json:"samples"`
}

// LoadBaselineSamples reads the JSON and returns the samples slice.
func LoadBaselineSamples(path string) ([]Sample, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bf baselineFile
	if err := json.Unmarshal(data, &bf); err != nil {
		return nil, err
	}
	return bf.Samples, nil
}
