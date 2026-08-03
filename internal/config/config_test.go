package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMinimalConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.yaml")
	yaml := `
accounts:
  prod:
    role_arn: acs:ram::123:role/ai-cloud-ops
    regions: [cn-hangzhou, cn-beijing]
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Accounts) != 1 {
		t.Errorf("got %d accounts, want 1", len(c.Accounts))
	}
	if c.Accounts["prod"].RoleARN != "acs:ram::123:role/ai-cloud-ops" {
		t.Errorf("got role_arn=%q", c.Accounts["prod"].RoleARN)
	}
	if len(c.Accounts["prod"].Regions) != 2 {
		t.Errorf("got %d regions, want 2", len(c.Accounts["prod"].Regions))
	}
}

func TestEmptyAccountsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(path, []byte("accounts: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for empty accounts, got nil")
	}
}

func TestEmptyRoleARNRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(`
accounts:
  prod:
    role_arn: ""
    regions: [cn-hangzhou]
`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for empty role_arn, got nil")
	}
}

func TestEmptyRegionsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(`
accounts:
  prod:
    role_arn: acs:ram::1:role/x
    regions: []
`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for empty regions, got nil")
	}
}

func TestEndpointForKnownRegion(t *testing.T) {
	acct := AccountConfig{RoleARN: "arn", Regions: []string{"cn-hangzhou"}}
	ep, err := EndpointFor("cms", "cn-hangzhou", acct)
	if err != nil {
		t.Fatalf("EndpointFor: %v", err)
	}
	if ep != "cms.cn-hangzhou.aliyuncs.com" {
		t.Errorf("got %q", ep)
	}
}

func TestEndpointForUnknownRegionErrors(t *testing.T) {
	acct := AccountConfig{RoleARN: "arn", Regions: []string{"mars-1"}}
	if _, err := EndpointFor("cms", "mars-1", acct); err == nil {
		t.Fatal("expected error for unknown region")
	}
}

func TestEndpointOverrideTakesPrecedence(t *testing.T) {
	acct := AccountConfig{
		RoleARN:           "arn",
		Regions:           []string{"cn-hangzhou"},
		EndpointOverrides: map[string]string{"cms.cn-hangzhou": "cms.internal.example.com"},
	}
	ep, err := EndpointFor("cms", "cn-hangzhou", acct)
	if err != nil {
		t.Fatalf("EndpointFor: %v", err)
	}
	if ep != "cms.internal.example.com" {
		t.Errorf("got %q, want override", ep)
	}
}

func TestSTSEndpointIsRegionAgnostic(t *testing.T) {
	acct1 := AccountConfig{RoleARN: "arn", Regions: []string{"cn-hangzhou"}}
	acct2 := AccountConfig{RoleARN: "arn", Regions: []string{"cn-beijing"}}
	ep1, _ := EndpointFor("sts", "cn-hangzhou", acct1)
	ep2, _ := EndpointFor("sts", "cn-beijing", acct2)
	if ep1 != ep2 {
		t.Errorf("STS should be region-agnostic; got %q vs %q", ep1, ep2)
	}
}