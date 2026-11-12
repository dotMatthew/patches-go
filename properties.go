package main

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The old config format (patches.properties of the Java version) is only
// read: it is migrated to TOML on first use (see migrateConfig).

// readPropertiesConfig reads a patches.properties file.
func readPropertiesConfig(data []byte) (*config, error) {
	p, err := parseProperties(data)
	if err != nil {
		return nil, err
	}
	url, okURL := p["baseRepoUrl"]
	ref, okRef := p["baseRepoRef"]
	dir, okDir := p["patchesDirectoryPath"]
	if !okDir {
		dir = defaultPatchesDir
	}
	if !okURL || !okRef {
		return nil, errors.New("baseRepoUrl and baseRepoRef are required")
	}
	return &config{URL: url, Ref: ref, PatchesDir: dir}, nil
}

// parseProperties implements the java.util.Properties file format
// (ISO-8859-1, comments, line continuations, escapes, "=", ":" or blank as
// separator).
func parseProperties(data []byte) (map[string]string, error) {
	isBlank := func(c uint16) bool { return c == ' ' || c == '\t' || c == '\f' }
	var logical [][]uint16
	var cur []uint16
	continuing := false
	for _, natural := range strings.FieldsFunc(strings.ReplaceAll(string(data), "\r\n", "\n"), func(r rune) bool { return r == '\n' || r == '\r' }) {
		line := make([]uint16, 0, len(natural))
		for i := 0; i < len(natural); i++ {
			line = append(line, uint16(natural[i])) // ISO-8859-1: one byte = one char
		}
		for len(line) > 0 && isBlank(line[0]) {
			line = line[1:]
		}
		if !continuing && (len(line) == 0 || line[0] == '#' || line[0] == '!') {
			continue
		}
		backslashes := 0
		for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
			backslashes++
		}
		continuing = backslashes%2 == 1
		if continuing {
			line = line[:len(line)-1]
		}
		cur = append(cur, line...)
		if !continuing {
			logical = append(logical, cur)
			cur = nil
		}
	}
	if continuing {
		logical = append(logical, cur)
	}

	result := map[string]string{}
	for _, line := range logical {
		keyEnd, valueStart, hasSep, escaped := len(line), len(line), false, false
		for i, c := range line {
			if !escaped && (c == '=' || c == ':' || isBlank(c)) {
				keyEnd, valueStart, hasSep = i, i+1, c != ' ' && c != '\t' && c != '\f'
				break
			}
			escaped = c == '\\' && !escaped
		}
		for valueStart < len(line) {
			c := line[valueStart]
			if !isBlank(c) && (hasSep || (c != '=' && c != ':')) {
				break
			}
			hasSep = hasSep || !isBlank(c)
			valueStart++
		}
		key, err := unescapeProperty(line[:keyEnd])
		if err != nil {
			return nil, err
		}
		value, err := unescapeProperty(line[valueStart:])
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func unescapeProperty(in []uint16) (string, error) {
	var out []uint16
	for i := 0; i < len(in); i++ {
		if in[i] != '\\' {
			out = append(out, in[i])
			continue
		}
		if i++; i == len(in) {
			break
		}
		switch c := in[i]; c {
		case 'u':
			if i+5 > len(in) {
				return "", errors.New("malformed \\uxxxx encoding")
			}
			v, err := strconv.ParseUint(string(utf16.Decode(in[i+1:i+5])), 16, 16)
			if err != nil {
				return "", errors.New("malformed \\uxxxx encoding")
			}
			out = append(out, uint16(v))
			i += 4
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'n':
			out = append(out, '\n')
		case 'f':
			out = append(out, '\f')
		default:
			out = append(out, c)
		}
	}
	return string(utf16.Decode(out)), nil
}
