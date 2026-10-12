package maintenance

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
	"reliquary/internal/utils"
)

// Credential values live only in memory. Password hashes for local owner login
// are intentionally absent from this inventory.
func credentialNeedles(ctx context.Context, db *gorm.DB) ([]string, error) {
	columns, err := Schema(ctx, db)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, column := range columns {
		present[column.Table] = true
	}
	values := map[string]bool{}
	var add func(any)
	add = func(value any) {
		switch value := value.(type) {
		case string:
			if value == "" {
				return
			}
			values[value] = true
			if strings.HasPrefix(strings.ToLower(value), "bearer ") || strings.HasPrefix(strings.ToLower(value), "basic ") {
				if _, token, ok := strings.Cut(value, " "); ok && token != "" {
					values[token] = true
				}
			}
			var decoded any
			if json.Unmarshal([]byte(value), &decoded) == nil {
				add(decoded)
			}
			plain, ok := utils.DecryptStoredSecretLenient(value)
			if ok && plain != "" && plain != value {
				values[plain] = true
				var decoded any
				if json.Unmarshal([]byte(plain), &decoded) == nil {
					add(decoded)
				}
			}
		case map[string]any:
			for key, child := range value {
				if publicCredentialMetadata(key) || safeHeader(key) {
					continue
				}
				add(child)
			}
		case []any:
			for _, child := range value {
				add(child)
			}
		}
	}
	for _, variable := range os.Environ() {
		key, value, _ := strings.Cut(variable, "=")
		if sensitiveKey(key) {
			add(value)
		} else {
			collectSensitive(value, "", add)
		}
	}
	for _, table := range []string{"models", "data_sources", "spaces", "system_settings", "chatgpt_accounts", "chatgpt_login_attempts", "api_keys", "auth_tokens", "resource_access_grants"} {
		if !present[table] {
			continue
		}
		var encoded []string
		if err := db.WithContext(ctx).Raw("SELECT to_jsonb(t)::text FROM public." + quote(table) + " t").Scan(&encoded).Error; err != nil {
			return nil, err
		}
		rows := []map[string]any{}
		for _, encoded := range encoded {
			row, err := jsonRow([]byte(encoded))
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
		oauthStates := map[string]bool{}
		if table == "chatgpt_login_attempts" {
			oauthStates = inertOAuthStates(rows)
		}
		for _, row := range rows {
			omitInertAuthenticationCredentials(table, row, oauthStates)
			collectSensitive(row, "", add)
			for _, column := range strings.Fields(SecretColumns[table]) {
				add(row[column])
			}
			if table == "system_settings" && row["is_secret"] == true {
				add(row["value"])
			}
		}
	}
	result := []string{}
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return len(result[i]) > len(result[j]) })
	return result, nil
}

func publicCredentialMetadata(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
	case "username", "user", "email", "scope", "scopes", "token_type", "expires_at", "expires_in", "expiry", "created_at", "updated_at", "account_id", "client_id", "subject", "issuer", "name", "display_name", "description", "type", "provider":
		return true
	}
	return false
}

func collectSensitive(value any, path string, add func(any)) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if sensitiveKey(key) || (headerContainer(path) && !safeHeader(key)) {
				add(child)
				continue
			}
			collectSensitive(child, path+"."+key, add)
		}
	case []any:
		for _, child := range value {
			collectSensitive(child, path, add)
		}
	case string:
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return
		}
		if parsed.User != nil {
			add(parsed.User.String())
			if password, ok := parsed.User.Password(); ok {
				add(password)
			}
		}
		for key, values := range parsed.Query() {
			if sensitiveURLKey(key) {
				for _, value := range values {
					add(value)
				}
			}
		}
	}
}

func redactCredentials(value any, needles []string) any {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			value[key] = redactCredentials(child, needles)
		}
		return value
	case []any:
		for i, child := range value {
			value[i] = redactCredentials(child, needles)
		}
		return value
	case string:
		for _, needle := range needles {
			// Short credentials are excluded by their explicit fields and the
			// package scan. Replacing tiny substrings would destroy ordinary text.
			if len(needle) >= 8 || value == needle {
				value = strings.ReplaceAll(value, needle, "[credential excluded]")
			}
		}
		return value
	}
	return value
}

func scanCredentialReader(reader io.Reader, needles []string) error {
	max := 1
	for _, needle := range needles {
		if len(needle) > max {
			max = len(needle)
		}
	}
	buffer := make([]byte, 1024*1024)
	var tail []byte
	for {
		n, err := reader.Read(buffer)
		data := append(tail, buffer[:n]...)
		for _, needle := range needles {
			if bytes.Contains(data, []byte(needle)) {
				return errors.New("operational credential detected")
			}
		}
		if len(data) > max {
			data = data[len(data)-max:]
		}
		tail = append([]byte{}, data...)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// ScanCredentialReader checks decoded database dumps without writing credentials
// to a file or emitting them through stdout, stderr or its error.
func ScanCredentialReader(ctx context.Context, db *gorm.DB, input io.Reader) error {
	needles, err := credentialNeedles(ctx, db)
	if err != nil {
		return errors.New("could not load credential inventory for verification")
	}
	return scanCredentialReader(input, needles)
}

// ScanCredentialFiles rejects known plaintext or encrypted credentials in raw
// files, Docker gzip layers and Git history. No credential value is returned.
func ScanCredentialFiles(ctx context.Context, db *gorm.DB, root string) error {
	needles, err := credentialNeedles(ctx, db)
	if err != nil {
		return errors.New("could not load credential inventory for verification")
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink refused during credential scan")
		}
		relative, _ := filepath.Rel(root, path)
		if err := scanCredentialReader(strings.NewReader(relative), needles); err != nil {
			return errors.New("credential integrity scan failed: asset name contains a credential")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		err = scanCredentialReader(file, needles)
		file.Close()
		if err == nil && strings.HasSuffix(path, ".tar") {
			err = scanTar(path, needles)
		}
		if err == nil && strings.HasSuffix(path, ".bundle") {
			err = scanGitBundle(ctx, path, needles)
		}
		if err != nil {
			for _, needle := range needles {
				relative = strings.ReplaceAll(relative, needle, "[credential excluded]")
			}
			return fmt.Errorf("credential integrity scan failed: %s", relative)
		}
		return nil
	})
}

func scanTar(path string, needles []string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	archive := tar.NewReader(file)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		reader := bufio.NewReader(archive)
		signature, _ := reader.Peek(2)
		if bytes.Equal(signature, []byte{0x1f, 0x8b}) {
			expanded, err := gzip.NewReader(reader)
			if err != nil {
				return err
			}
			err = scanCredentialReader(expanded, needles)
			expanded.Close()
			if err != nil {
				return err
			}
		} else if err := scanCredentialReader(reader, needles); err != nil {
			return err
		}
	}
}

func scanGitBundle(ctx context.Context, path string, needles []string) error {
	directory, err := os.MkdirTemp("", "reliquary-credential-scan-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := exec.CommandContext(ctx, "git", "clone", "--bare", "--quiet", path, directory).Run(); err != nil {
		return errors.New("could not inspect bundled Git history")
	}
	objects, err := exec.CommandContext(ctx, "git", "-C", directory, "rev-list", "--objects", "--all").Output()
	if err != nil {
		return errors.New("could not list bundled Git objects")
	}
	input := bytes.Buffer{}
	for _, line := range strings.Split(string(objects), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			input.WriteString(fields[0] + "\n")
		}
	}
	command := exec.CommandContext(ctx, "git", "-C", directory, "cat-file", "--batch")
	command.Stdin = &input
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	scanErr := scanCredentialReader(output, needles)
	if scanErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if scanErr != nil {
		return scanErr
	}
	return waitErr
}
