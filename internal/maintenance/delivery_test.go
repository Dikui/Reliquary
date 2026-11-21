package maintenance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeliveryResumeAcceptsCustomerChatGPTConfiguration(t *testing.T) {
	model := func(id, name, status, account string) map[string]any {
		extra := map[string]any{"maintenance_original_status": "active"}
		if account != "" {
			extra["chatgpt_account_id"] = account
		}
		return map[string]any{"id": id, "name": name, "display_name": name, "description": "", "status": status,
			"source": "chatgpt", "type": "KnowledgeQA", "updated_at": "2026-11-20T00:00:00Z",
			"parameters": map[string]any{"extra_config": extra}}
	}
	table := func(rows ...map[string]any) TableManifest {
		return TableManifest{Rows: int64(len(rows)), Data: rows}
	}
	expected := &Manifest{FormatVersion: FormatVersion, Delivery: true, Tables: map[string]TableManifest{
		"chatgpt_accounts": table(), "chatgpt_hosts": table(),
		"models": table(model("selected", "original slug", "needs_configuration", "")),
	}, Required: []Requirement{{Kind: "models", ID: "selected", Fields: []string{"chatgpt_login"}}}}
	actual := &Manifest{FormatVersion: FormatVersion, Tables: map[string]TableManifest{
		"chatgpt_accounts": table(map[string]any{"id": "customer", "status": "connected"}),
		"chatgpt_hosts":    table(map[string]any{"id": 1, "ext_agent_host_id": "customer-installation"}),
		"models":           table(model("selected", "available slug", "active", "customer"), model("new", "another slug", "active", "customer")),
	}}
	options := CompareOptions{AllowReconfigurationAppend: true}
	require.Empty(t, CompareManifestWithSettings(expected, actual, options))
	require.NotEmpty(t, CompareManifest(expected, actual))
	expected.Delivery = false
	require.Contains(t, CompareManifestWithSettings(expected, actual, options), "table chatgpt_accounts")
	expected.Delivery = true
	actual.Tables["models"].Data[0] = model("selected", "available slug", "active", "missing-account")
	require.Contains(t, CompareManifestWithSettings(expected, actual, options), "table models")
}

func TestDeliveryResumeAcceptsOnlyTheDefaultUserWhenSourceHasNoOwner(t *testing.T) {
	expected := &Manifest{FormatVersion: FormatVersion, Delivery: true, Tables: map[string]TableManifest{"users": {}}}
	user := map[string]any{"id": "customer-owner", "space_id": 1, "is_active": true, "avatar": "",
		"username": "me", "email": "me@localhost", "password_hash": ""}
	actual := &Manifest{FormatVersion: FormatVersion, Tables: map[string]TableManifest{
		"users": {Rows: 1, Data: []map[string]any{user}},
	}}
	options := CompareOptions{AllowReconfigurationAppend: true}
	require.Empty(t, CompareManifestWithSettings(expected, actual, options))
	user["email"] = "other@example.test"
	require.Contains(t, CompareManifestWithSettings(expected, actual, options), "table users")
}
