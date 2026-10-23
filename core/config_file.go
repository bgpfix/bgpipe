package core

import (
	"fmt"
	"os"
	"strings"
)

// spliceConfig replaces -f/--config FILE among the global options in args
// (ie. before the first stage) with the arguments read from FILE
func (b *Bgpipe) spliceConfig(args []string) ([]string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			return args, nil // first stage
		}

		// -f FILE, --config FILE, or --config=FILE?
		name, path, hasval := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name == "f" || name == "config" {
			end := i + 1
			if !hasval {
				if end >= len(args) {
					return nil, fmt.Errorf("%s: missing file path", arg)
				}
				path, end = args[end], end+1
			}
			cfg, err := readConfig(path)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", arg, path, err)
			}
			return append(append(args[:i:i], cfg...), args[end:]...), nil
		}

		// skip the value of a global option?
		f := b.F.Lookup(name)
		if len(name) == 1 {
			f = b.F.ShorthandLookup(name)
		}
		if f != nil && !hasval && f.NoOptDefVal == "" {
			i++
		}
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
		if c := line[0]; c != ' ' && c != '\t' && !strings.HasPrefix(words[0], "-") {
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
		case c == '\'':
			j := strings.IndexByte(line[i+1:], c)
			if j < 0 {
				return nil, fmt.Errorf("unterminated %c quote", c)
			}
			w.WriteString(line[i+1 : i+1+j])
			i += j + 1
		case c == '"': // NB: \" and \\ are escapes inside
			for i++; i < len(line) && line[i] != '"'; i++ {
				if line[i] == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\') {
					i++
				}
				w.WriteByte(line[i])
			}
			if i >= len(line) {
				return nil, fmt.Errorf("unterminated %c quote", c)
			}
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
