package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The .env format follows github.com/subosito/gotenv, which viper used
// before: KEY=value or KEY: value, an optional "export ", single or double
// quotes, # comments, and $VAR or ${VAR} expansion outside single quotes.
var (
	dotenvLine     = regexp.MustCompile(`\A\s*(?:export\s+)?([\w\.]+)(?:\s*=\s*|:\s+?)('(?:\'|[^'])*'|"(?:\"|[^"])*"|[^#\n]+)?\s*(?:\s*\#.*)?\z`)
	dotenvVar      = regexp.MustCompile(`(\\)?(\$)(\{?([A-Z0-9_]+)?\}?)`)
	dotenvVarName  = regexp.MustCompile(`(\$)(\{?([A-Z0-9_]+)\}?)`)
	dotenvUnescape = regexp.MustCompile(`\\([^$])`)
)

// readDotEnv reads path. It returns no values when the file is missing or
// any line is malformed, as viper did.
func readDotEnv(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	values, err := parseDotEnv(data)
	if err != nil {
		return nil
	}
	return values
}

// parseDotEnv returns the values by key. A quoted value can span lines.
func parseDotEnv(data []byte) (map[string]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	values := map[string]string{}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line[0] == '#' {
			continue
		}

		if quote := openQuote(line); quote != "" {
			for quote != "" && i+1 < len(lines) {
				i++
				line += "\n" + lines[i]
				if j := strings.LastIndex(lines[i], quote); j == 0 || j > 0 && lines[i][j-1] != '\\' {
					quote = ""
				}
			}
			if quote != "" {
				return nil, errors.New("missing quotes")
			}
		}

		if err := parseDotEnvLine(line, values); err != nil {
			return nil, err
		}
	}
	return values, nil
}

// openQuote returns the quote character of a value that does not close on
// its first line.
func openQuote(line string) string {
	idx := strings.Index(line, "=")
	if idx == -1 {
		idx = strings.Index(line, ":")
	}
	if idx <= 0 || idx >= len(line)-1 {
		return ""
	}
	val := strings.TrimSpace(line[idx+1:])
	if val[0] != '"' && val[0] != '\'' {
		return ""
	}
	quote := val[:1]
	if j := strings.LastIndex(strings.TrimSpace(val[1:]), quote); j >= 0 && val[j] != '\\' {
		return ""
	}
	return quote
}

func parseDotEnvLine(line string, values map[string]string) error {
	m := dotenvLine.FindStringSubmatch(line)
	if m == nil {
		// "export KEY" is allowed for a key set earlier in the file.
		if name, ok := strings.CutPrefix(line, "export "); ok {
			if _, set := values[name]; set {
				return nil
			}
		}
		return fmt.Errorf("line %q does not match the .env format", line)
	}

	key := strings.TrimSpace(m[1])
	val := strings.TrimSpace(m[2])

	singleQuoted := len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\''
	doubleQuoted := len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"'
	if singleQuoted || doubleQuoted {
		val = val[1 : len(val)-1]
	}
	if doubleQuoted {
		val = strings.ReplaceAll(val, `\n`, "\n")
		val = strings.ReplaceAll(val, `\r`, "\r")
		val = dotenvUnescape.ReplaceAllString(val, "$1")
	}
	if !singleQuoted {
		val = dotenvVar.ReplaceAllStringFunc(val, func(s string) string { return expandDotEnvVar(s, values) })
	}

	values[key] = val
	return nil
}

// expandDotEnvVar replaces $VAR or ${VAR}. The process environment wins
// over keys set earlier in the file. "\$" stays a literal "$".
func expandDotEnvVar(s string, values map[string]string) string {
	if s[0] == '\\' {
		return s[1:]
	}
	m := dotenvVarName.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	if v, ok := os.LookupEnv(m[3]); ok {
		return v
	}
	return values[m[3]]
}
