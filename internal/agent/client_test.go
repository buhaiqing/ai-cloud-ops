package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type mockMessageClient struct {
	responses []*anthropic.Message
	calls     []anthropic.MessageNewParams
	err       error
}

func (m *mockMessageClient) New(
	_ context.Context,
	params anthropic.MessageNewParams,
	_ ...option.RequestOption,
) (*anthropic.Message, error) {
	m.calls = append(m.calls, params)
	if m.err != nil {
		return nil, m.err
	}
	if len(m.responses) == 0 {
		return nil, errors.New("mock has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func validDiagnosisJSON() string {
	payload, _ := json.Marshal(map[string]any{
		"root_cause": "RDS CPU saturated due to a missing index",
		"recommendations": []map[string]any{{
			"action":           "add index",
			"command":          "aliyun rds CreateIndex",
			"expected_outcome": "query latency decreases",
		}},
		"evidence_chains": []map[string]any{{
			"claim":           "CPU exceeded 95%",
			"supporting_tool": "describe_cms_metric_list",
			"supporting_data": "avg=97.2",
		}},
		"confidence": "high",
		"caveats":    []string{"verify during business hours"},
	})
	return string(payload)
}

func textResponse(text string) *anthropic.Message {
	blockJSON, _ := json.Marshal(map[string]any{"type": "text", "text": text})
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal(blockJSON, &block); err != nil {
		panic(err)
	}
	return &anthropic.Message{Content: []anthropic.ContentBlockUnion{block}}
}

func toolUseResponse(id string) *anthropic.Message {
	blockJSON, _ := json.Marshal(map[string]any{
		"type":  "tool_use",
		"id":    id,
		"name":  "describe_ecs_instances",
		"input": map[string]any{"region": "cn-hangzhou"},
	})
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal(blockJSON, &block); err != nil {
		panic(err)
	}
	return &anthropic.Message{Content: []anthropic.ContentBlockUnion{block}}
}

func testClient(messages messageCreator) *Client {
	return &Client{model: "claude-test", messageClient: messages}
}

func TestDiagnoseSuccessfulTextResponse(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{textResponse(validDiagnosisJSON())}}
	diagnosis, err := testClient(mock).Diagnose(context.Background(), map[string]any{"alert_id": "a-1"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if diagnosis.Confidence != "high" || !strings.Contains(diagnosis.RootCause, "RDS CPU") {
		t.Fatalf("Diagnose() = %#v", diagnosis)
	}
	if diagnosis.Model != "claude-test" || diagnosis.PromptVersion != PromptVersion {
		t.Fatalf("diagnosis metadata = %#v", diagnosis)
	}
}

func TestDiagnoseToolUseLoop(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseResponse("toolu-1"),
		textResponse(validDiagnosisJSON()),
	}}
	diagnosis, err := testClient(mock).Diagnose(context.Background(), map[string]any{"alert_id": "a-2"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if diagnosis.Confidence != "high" || len(mock.calls) != 2 {
		t.Fatalf("diagnosis = %#v, calls = %d", diagnosis, len(mock.calls))
	}
	secondRequest, err := json.Marshal(mock.calls[1].Messages)
	if err != nil {
		t.Fatalf("marshal second request: %v", err)
	}
	for _, want := range []string{"tool_result", "toolu-1", "tool_not_implemented_in_m1"} {
		if !strings.Contains(string(secondRequest), want) {
			t.Fatalf("second request missing %q: %s", want, secondRequest)
		}
	}
}

func TestDiagnoseCapsToolLoopAtFiveIterations(t *testing.T) {
	responses := make([]*anthropic.Message, maxToolIterations)
	for i := range responses {
		responses[i] = toolUseResponse("toolu-cap")
	}
	mock := &mockMessageClient{responses: responses}
	diagnosis, err := testClient(mock).Diagnose(context.Background(), map[string]any{"alert_id": "a-3"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if got := len(mock.calls); got != maxToolIterations {
		t.Fatalf("Messages.New calls = %d, want %d", got, maxToolIterations)
	}
	if diagnosis.Confidence != "low" || diagnosis.RootCause != "diagnosis_truncated" {
		t.Fatalf("Diagnose() = %#v", diagnosis)
	}
}

func TestDiagnoseParseErrorReturnsLowConfidence(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{textResponse("not-json")}}
	diagnosis, err := testClient(mock).Diagnose(context.Background(), map[string]any{"alert_id": "a-4"})
	if err == nil {
		t.Fatal("Diagnose() error = nil, want parse error")
	}
	if diagnosis.Confidence != "low" || len(diagnosis.Caveats) != 1 || diagnosis.Caveats[0] != "inference failed" {
		t.Fatalf("Diagnose() = %#v", diagnosis)
	}
}
