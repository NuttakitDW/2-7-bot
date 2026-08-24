package arena

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// APIKeyEnv is the environment variable holding the arena API key.
const APIKeyEnv = "API_KEY"

// ResolveAPIKey returns the API key from the environment, falling back to a
// .env file in the working directory.
//
// The key is a credential: it is read from the environment or an ignored .env
// file, never committed, and never logged.
func ResolveAPIKey(dotenvPath string) (string, error) {
	if key := strings.TrimSpace(os.Getenv(APIKeyEnv)); key != "" {
		return key, nil
	}

	values, err := readDotEnv(dotenvPath)
	if err != nil {
		return "", err
	}
	if key := strings.TrimSpace(values[APIKeyEnv]); key != "" {
		return key, nil
	}

	return "", fmt.Errorf("no %s found: export it, or put %s=<key> in %s",
		APIKeyEnv, APIKeyEnv, dotenvPath)
}

// readDotEnv parses a minimal KEY=VALUE file. A missing file is not an error —
// the environment is the primary source and .env is only a convenience.
func readDotEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(key)] = trimQuotes(strings.TrimSpace(value))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return values, nil
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
