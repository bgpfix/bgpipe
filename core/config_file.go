package core

import (
	"fmt"
	"os"
	"strings"
)

// spliceConfig replaces the first -f/--config FILE (before any "--") in args
// with the arguments read from FILE (see readConfig)
func spliceConfig(args []string) ([]string, error) {
	for i, arg := range args {
		var path string
		switch {
		case arg == "--":
			return args, nil
		case (arg == "-f" || arg == "--config") && i+1 < len(args):
			path = args[i+1]
		case strings.HasPrefix(arg, "--config="):
			path = arg[9:]
		default:
			continue
		}

		cfg, err := readConfig(path)
		if err != nil {
			return nil, fmt.Errorf("--config %s: %w", path, err)
		}
		end := i + 1
		if !strings.HasPrefix(arg, "--config=") {
			end++
		}
		return append(append(args[:i:i], cfg...), args[end:]...), nil
	}
	return args, nil
}

// readConfig reads CLI arguments from a file, with shell-like quoting,
// # comments, and \ line continuations. A line that starts with a word
// (not indented, not an option) begins a new stage, ie. implies "--".
func readConfig(path string) (args []string, err error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(buf), "\\\n", " ")
	for n, line := range strings.Split(text, "\n") {
		words, err := splitWords(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		} else if len(args) == 0 && len(words) > 0 && words[0] == "bgpipe" {
			words = words[1:] // copy-pasted CLI
		}
		if len(words) == 0 {
			continue
		}
		if c := line[0]; c != ' ' && c != '\t' && words[0][0] != '-' {
			args = append(args, "--")
		}
		args = append(args, words...)
	}
	return args, nil
}

// splitWords splits line like a shell would: on spaces, with single and double quotes,
// \ escapes, and # comments
func splitWords(line string) (words []string, err error) {
	var w strings.Builder
	inword := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == ' ' || c == '\t' || c == '\r':
			if inword {
				words, inword = append(words, w.String()), false
				w.Reset()
			}
			continue
		case c == '#' && !inword:
			return words, nil
		case c == '\\' && i+1 < len(line):
			i++
			w.WriteByte(line[i])
		case c == '\'' || c == '"':
			j := strings.IndexByte(line[i+1:], c)
			if j < 0 {
				return nil, fmt.Errorf("unterminated %c quote", c)
			}
			w.WriteString(line[i+1 : i+1+j])
			i += j + 1
		default:
			w.WriteByte(c)
		}
		inword = true
	}
	if inword {
		words = append(words, w.String())
	}
	return words, nil
}
