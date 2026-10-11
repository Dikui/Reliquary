package maintenance

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"reliquary/internal/utils"
)

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	compact := strings.ReplaceAll(key, "_", "")
	switch compact {
	case "apikey", "keyhash", "appsecret", "clientsecret", "password", "passwordhash", "passwd", "secret", "token", "accesstoken", "refreshtoken", "idtoken", "authorization", "proxyauthorization", "cookie", "setcookie", "credentials", "credential", "privatekey", "systemaeskey", "jwtsecret", "nonce", "verifier", "accesskey", "accesskeyid", "awsaccesskeyid", "secretaccesskey", "secretkey", "secretid", "xamzsignature", "awssignature", "xamzcredential":
		return true
	}
	return strings.HasSuffix(key, "_api_key") || strings.HasSuffix(key, "_password") || strings.HasSuffix(key, "_secret") || strings.HasSuffix(key, "_token") || strings.HasSuffix(key, "_access_key") || strings.HasSuffix(key, "_access_key_id") || strings.HasSuffix(key, "_secret_key") || strings.HasSuffix(key, "_secret_id")
}

func sensitiveURLKey(key string) bool {
	return sensitiveKey(key) || strings.EqualFold(key, "signature") || strings.EqualFold(key, "sig")
}

// Sanitize recursively removes credentials from JSON, HTTP headers and URL
// userinfo/query parameters. It never decrypts a credential while exporting.
func Sanitize(value any) (any, []string) { return sanitize(value, "") }
func sanitize(value any, path string) (any, []string) {
	var removed []string
	switch value := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range value {
			child := key
			if path != "" {
				child = path + "." + key
			}
			if sensitiveKey(key) || (headerContainer(path) && !safeHeader(key)) {
				if nonempty(item) {
					removed = append(removed, child)
				}
				continue
			}
			clean, paths := sanitize(item, child)
			out[key] = clean
			removed = append(removed, paths...)
		}
		sort.Strings(removed)
		return out, removed
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			clean, paths := sanitize(item, path)
			out[i] = clean
			removed = append(removed, paths...)
		}
		return out, removed
	case string:
		trim := strings.TrimSpace(value)
		if originalTextPath(path) {
			return value, nil
		}
		if u, err := url.Parse(trim); err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "postgres" || u.Scheme == "postgresql") && u.Host != "" {
			if u.User != nil {
				u.User = nil
				removed = append(removed, path+".userinfo")
			}
			query := u.Query()
			for key := range query {
				if sensitiveURLKey(key) {
					query.Del(key)
					removed = append(removed, path+".query."+key)
				}
			}
			u.RawQuery = query.Encode()
			if len(removed) > 0 {
				return u.String(), removed
			}
		}
		return value, nil
	default:
		return value, nil
	}
}

func originalTextPath(path string) bool {
	if path == "vault_body" {
		return true
	}
	for _, suffix := range []string{".original_content", ".raw_content", ".manual.content", ".intake.content"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func headerContainer(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), "custom_headers") || strings.HasSuffix(strings.ToLower(path), ".headers") || strings.EqualFold(path, "headers")
}

func safeHeader(key string) bool {
	return strings.EqualFold(key, "Accept") || strings.EqualFold(key, "Content-Type") || strings.EqualFold(key, "User-Agent")
}

type Requirement struct {
	Kind   string   `json:"kind"`
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
	Reason string   `json:"reason"`
}
type Requirements struct {
	Required      []Requirement `json:"required"`
	PausedModels  []string      `json:"paused_models"`
	PausedSources []string      `json:"paused_sources"`
}

func sanitizeRow(table string, row map[string]any) (map[string]any, []string) {
	copy := map[string]any{}
	var removed []string
	for column, value := range row {
		if secretColumn(table, column) {
			if nonempty(value) {
				removed = append(removed, column)
			}
			continue
		}
		// Login password hashes are intentionally retained. They cannot grant an
		// existing session, and preserve the user's ability to log in after loss.
		if table == "users" && column == "password_hash" {
			copy[column] = value
			continue
		}
		if table == "system_settings" && column == "value" && (row["is_secret"] == true || sensitiveKey(fmt.Sprint(row["key"]))) {
			copy[column] = ""
			removed = append(removed, "value")
			continue
		}
		clean, paths := sanitize(value, column)
		if table == "data_sources" && column == "config" {
			if config, ok := value.(map[string]any); ok {
				expanded := []string{}
				for _, field := range paths {
					if field == "config.credentials" {
						expanded = append(expanded, credentialLeafPaths(config["credentials"], field)...)
					} else {
						expanded = append(expanded, field)
					}
				}
				paths = expanded
			}
		}
		copy[column] = clean
		removed = append(removed, paths...)
	}
	sort.Strings(removed)
	return copy, removed
}

// Credential containers are excluded in full, but reminders name every
// original nonempty leaf so restoring one field cannot enable an incomplete
// subscription. Decrypted values stay in memory; only their field names leave.
func credentialLeafPaths(value any, path string) []string {
	if text, ok := value.(string); ok {
		if plain, valid := utils.DecryptStoredSecretLenient(text); valid {
			var decoded map[string]any
			if json.Unmarshal([]byte(plain), &decoded) == nil && decoded != nil {
				value = decoded
			}
		}
	}
	if object, ok := value.(map[string]any); ok {
		fields := []string{}
		for key, child := range object {
			if nonempty(child) {
				fields = append(fields, credentialLeafPaths(child, path+"."+key)...)
			}
		}
		sort.Strings(fields)
		return fields
	}
	if nonempty(value) {
		return []string{path}
	}
	return nil
}

func nonempty(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return text != ""
	}
	return true
}

func markModelRequirements(row map[string]any, fields []string) {
	parameters, ok := row["parameters"].(map[string]any)
	if !ok {
		return
	}
	extra, ok := parameters["extra_config"].(map[string]any)
	if !ok {
		extra = map[string]any{}
		parameters["extra_config"] = extra
	}
	paths := []string{}
	for _, field := range fields {
		paths = append(paths, strings.TrimPrefix(field, "parameters."))
	}
	sort.Strings(paths)
	data, _ := json.Marshal(paths)
	extra["maintenance_required_fields"] = string(data)
	if _, exists := extra["maintenance_original_status"]; !exists {
		extra["maintenance_original_status"] = fmt.Sprint(row["status"])
		extra["maintenance_original_updated_at"] = fmt.Sprint(row["updated_at"])
	}
}

func markSourceRequirements(row map[string]any, fields []string) {
	config, ok := row["config"].(map[string]any)
	if !ok {
		return
	}
	data, _ := json.Marshal(fields)
	config["_reliquary_maintenance_required_fields"] = string(data)
	if _, exists := config["_reliquary_maintenance_original_status"]; !exists {
		config["_reliquary_maintenance_original_status"] = fmt.Sprint(row["status"])
		config["_reliquary_maintenance_original_updated_at"] = fmt.Sprint(row["updated_at"])
	}
}

func requirementFields(value any) []string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	var fields []string
	if json.Unmarshal([]byte(text), &fields) != nil {
		return nil
	}
	return fields
}

func jsonRow(data []byte) (map[string]any, error) {
	var row map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	err := decoder.Decode(&row)
	return row, err
}
