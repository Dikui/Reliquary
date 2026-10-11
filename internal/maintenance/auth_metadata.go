package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

// This explicit non-authentication marker is neither a JWT nor a SHA256 token
// hash. It satisfies legacy NOT NULL constraints without preserving a secret.
const excludedCredentialPrefix = types.MaintenanceExcludedCredentialPrefix

func inactiveRequirement(table string, row map[string]any) Requirement {
	return Requirement{Kind: table, ID: rowIdentity(row), Name: "prior authentication record", Fields: strings.Fields(SecretColumns[table]), Reason: "old authentication is invalidated; sign in again or create a new access grant"}
}

func oauthMetadataIdentity(row map[string]any) string {
	metadata := map[string]any{}
	for column, value := range row {
		if !secretColumn("chatgpt_login_attempts", column) {
			metadata[column] = value
		}
	}
	data, _ := json.Marshal(metadata)
	return sum(data)
}

func restoreInactiveCredentials(table string, row map[string]any, occurrences map[string]int) {
	switch table {
	case "auth_tokens":
		row["token"] = excludedCredentialPrefix + fmt.Sprint(row["id"])
	case "resource_access_grants":
		row["token_hash"] = excludedCredentialPrefix + fmt.Sprint(row["id"])
	case "chatgpt_login_attempts":
		identity := oauthMetadataIdentity(row)
		occurrences[identity]++
		// Equal public metadata may belong to several distinct old attempts.
		// Preserve every occurrence without hashing the excluded old state.
		row["state"] = fmt.Sprintf("%s%s-%06d", excludedCredentialPrefix, identity, occurrences[identity])
		row["nonce"], row["verifier"] = "", ""
	}
}

func inertOAuthStates(rows []map[string]any) map[string]bool {
	counts := map[string]int{}
	for _, row := range rows {
		if row["nonce"] == "" && row["verifier"] == "" {
			clean, _ := sanitizeRow("chatgpt_login_attempts", row)
			counts[oauthMetadataIdentity(clean)]++
		}
	}
	states := map[string]bool{}
	for identity, count := range counts {
		for occurrence := 1; occurrence <= count; occurrence++ {
			states[fmt.Sprintf("%s%s-%06d", excludedCredentialPrefix, identity, occurrence)] = true
		}
	}
	return states
}

// Only values exactly generated for this row's public identity are inert.
// A lookalike prefix, an OAuth verifier, or a still-usable API key remains a
// credential. This permits another safe backup of a restored environment.
func omitInertAuthenticationCredentials(table string, row map[string]any, oauthStates map[string]bool) {
	id := fmt.Sprint(row["id"])
	switch table {
	case "auth_tokens":
		if row["token"] == excludedCredentialPrefix+id {
			delete(row, "token")
		}
	case "resource_access_grants":
		if row["token_hash"] == excludedCredentialPrefix+id {
			delete(row, "token_hash")
		}
	case "tenant_api_keys":
		if row["api_key"] == "" && row["key_hash"] == "disabled-"+id {
			delete(row, "key_hash")
		}
	case "chatgpt_login_attempts":
		state, _ := row["state"].(string)
		clean, _ := sanitizeRow("chatgpt_login_attempts", row)
		ownIdentity := excludedCredentialPrefix + oauthMetadataIdentity(clean) + "-"
		if row["nonce"] == "" && row["verifier"] == "" && strings.HasPrefix(state, ownIdentity) && oauthStates[state] {
			delete(row, "state")
		}
	}
}

// The OAuth state is the old primary key and must not leave the old environment.
// Replacing the disposable clone's rows assigns deterministic public identities
// and also avoids transient primary-key collisions when scrubbing a second time.
func scrubLoginAttemptMetadata(ctx context.Context, db *gorm.DB, needles []string) ([]Requirement, error) {
	var encoded []string
	if err := db.WithContext(ctx).Raw("SELECT to_jsonb(t)::text FROM public.chatgpt_login_attempts t").Scan(&encoded).Error; err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, data := range encoded {
		row, err := jsonRow([]byte(data))
		if err != nil {
			return nil, err
		}
		clean, _ := sanitizeRow("chatgpt_login_attempts", row)
		rows = append(rows, redactCredentials(clean, needles).(map[string]any))
	}
	sortRows(rows)
	if err := db.Exec("DELETE FROM public.chatgpt_login_attempts").Error; err != nil {
		return nil, err
	}
	required := []Requirement{}
	occurrences := map[string]int{}
	for _, row := range rows {
		required = append(required, inactiveRequirement("chatgpt_login_attempts", row))
		restoreInactiveCredentials("chatgpt_login_attempts", row, occurrences)
		if err := db.Exec(`INSERT INTO public.chatgpt_login_attempts(state,nonce,verifier,callback_url,client_id,account_id,expires_at) VALUES(?,?,?,?,?,?,?)`, row["state"], row["nonce"], row["verifier"], row["callback_url"], row["client_id"], row["account_id"], row["expires_at"]).Error; err != nil {
			return nil, err
		}
	}
	return required, nil
}
