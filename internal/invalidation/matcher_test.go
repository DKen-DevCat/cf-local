package invalidation

import (
	"strings"
	"testing"
)

func TestParsePattern(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantErr  string // substring match; "" means expect success
		wildcard bool
		prefix   string
	}{
		// --- happy paths ---------------------------------------------------
		{"trailing wildcard with slash", "/posts/*", "", true, "/posts/"},
		{"trailing wildcard without slash", "/foo*", "", true, "/foo"},
		{"root wildcard", "/*", "", true, "/"},
		{"literal exact path", "/posts/foo", "", false, "/posts/foo"},
		{"literal with file extension", "/index.html", "", false, "/index.html"},
		// AWS spec: `*` not at the end is treated as a literal character.
		{"asterisk in middle is literal", "/foo*.jpg", "", false, "/foo*.jpg"},
		{"asterisk near start is literal", "/*.jpg", "", false, "/*.jpg"},
		// AWS spec: trailing `*` is wildcard regardless of preceding chars.
		{"path with extension ending in star", "/images/image.jpg*", "", true, "/images/image.jpg"},
		// Query strings: cf-local treats `?` as part of the path string.
		{"literal with query", "/images/image.jpg?param=a", "", false, "/images/image.jpg?param=a"},
		{"wildcard after query separator", "/images/image.jpg*", "", true, "/images/image.jpg"},
		// Valid percent-encodings.
		{"valid percent escape", "/foo%20bar", "", false, "/foo%20bar"},
		{"multiple percent escapes", "/a%2Fb%2Fc", "", false, "/a%2Fb%2Fc"},
		{"percent escape with wildcard", "/foo%20*", "", true, "/foo%20"},
		// At max length (4000 chars total: '/' + 3999 'a').
		{"max length 4000", "/" + strings.Repeat("a", MaxPathLen-1), "", false, "/" + strings.Repeat("a", MaxPathLen-1)},

		// --- structural errors --------------------------------------------
		{"empty", "", "empty", false, ""},
		{"missing leading slash literal", "foo", "must begin with", false, ""},
		{"missing leading slash wildcard", "foo*", "must begin with", false, ""},
		{"too long by one", strings.Repeat("a", MaxPathLen+1), "exceeds", false, ""},

		// --- ~ rejection (literal and URL-encoded) ------------------------
		{"literal tilde middle", "/foo~bar", "~", false, ""},
		{"literal tilde leading", "/~foo", "~", false, ""},
		{"url-encoded tilde upper", "/foo%7Ebar", "~", false, ""},
		{"url-encoded tilde lower", "/foo%7ebar", "~", false, ""},

		// --- non-ASCII rejection ------------------------------------------
		{"non-ASCII utf8 multibyte", "/日本語", "non-ASCII", false, ""},
		{"non-ASCII single high byte", "/foo\xc3\xa9", "non-ASCII", false, ""},

		// --- RFC 1738 unsafe characters -----------------------------------
		{"space unencoded", "/foo bar", "unsafe character", false, ""},
		{"angle bracket open", "/foo<bar", "unsafe character", false, ""},
		{"angle bracket close", "/foo>bar", "unsafe character", false, ""},
		{"double quote", "/foo\"bar", "unsafe character", false, ""},
		{"backtick", "/foo`bar", "unsafe character", false, ""},
		{"pipe", "/foo|bar", "unsafe character", false, ""},
		{"caret", "/foo^bar", "unsafe character", false, ""},
		{"backslash", "/foo\\bar", "unsafe character", false, ""},
		{"left brace", "/foo{bar", "unsafe character", false, ""},
		{"right brace", "/foo}bar", "unsafe character", false, ""},
		{"left bracket", "/foo[bar", "unsafe character", false, ""},
		{"right bracket", "/foo]bar", "unsafe character", false, ""},

		// --- malformed URL escapes ----------------------------------------
		{"escape EOF", "/foo%", "incomplete URL escape", false, ""},
		{"escape one digit then EOF", "/foo%2", "incomplete URL escape", false, ""},
		{"escape non-hex first", "/foo%GH", "invalid URL escape", false, ""},
		{"escape non-hex second", "/foo%2G", "invalid URL escape", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePattern(tc.in)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ParsePattern(%q) unexpected error: %v", tc.in, err)
				}
				if got := p.IsWildcard(); got != tc.wildcard {
					t.Errorf("IsWildcard = %v, want %v", got, tc.wildcard)
				}
				if got := p.Prefix(); got != tc.prefix {
					t.Errorf("Prefix = %q, want %q", got, tc.prefix)
				}
				if got := p.String(); got != tc.in {
					t.Errorf("String = %q, want %q (raw input)", got, tc.in)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParsePattern(%q) expected error containing %q, got nil", tc.in, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestPatternMatch(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		// --- wildcard `/posts/*` (with trailing slash) --------------------
		{"wildcard slash matches deeper", "/posts/*", "/posts/foo", true},
		{"wildcard slash matches empty suffix", "/posts/*", "/posts/", true},
		{"wildcard slash matches deep nested", "/posts/*", "/posts/2026/05/foo", true},
		{"wildcard slash does not match shorter", "/posts/*", "/posts", false},
		{"wildcard slash does not match unrelated", "/posts/*", "/articles/foo", false},

		// --- wildcard `/foo*` (no trailing slash, AWS spec example) -------
		// AWS doc: "/directory-path/initial-characters-in-file-name*" matches
		// any file whose name starts with the given characters.
		{"wildcard prefix matches /foo", "/foo*", "/foo", true},
		{"wildcard prefix matches /foo/", "/foo*", "/foo/", true},
		{"wildcard prefix matches /foo/bar", "/foo*", "/foo/bar", true},
		{"wildcard prefix matches /foobar", "/foo*", "/foobar", true},
		{"wildcard prefix does not match /bar", "/foo*", "/bar", false},
		{"wildcard prefix does not match /fo", "/foo*", "/fo", false},

		// --- root wildcard `/*` -------------------------------------------
		{"root wildcard matches root", "/*", "/", true},
		{"root wildcard matches anything", "/*", "/anything", true},
		{"root wildcard matches deep", "/*", "/foo/bar/baz", true},

		// --- literal exact-match ------------------------------------------
		{"literal exact match", "/posts/foo", "/posts/foo", true},
		{"literal not matching subpath", "/posts/foo", "/posts/foo/bar", false},
		{"literal not matching prefix", "/posts/foo", "/posts", false},

		// --- literal with `*` (AWS treats as literal char) ----------------
		{"literal star matches itself", "/foo*.jpg", "/foo*.jpg", true},
		{"literal star not glob", "/foo*.jpg", "/foobar.jpg", false},
		{"literal star not exact", "/foo*.jpg", "/foo.jpg", false},

		// --- query strings in pattern -------------------------------------
		// CloudFront docs: "/images/image.jpg?parameter1=a" is a valid
		// invalidation path when query strings are forwarded.
		{"literal with query exact", "/foo?a=1", "/foo?a=1", true},
		{"literal with query no match", "/foo?a=1", "/foo?a=2", false},
		{"wildcard catches query variants", "/foo*", "/foo?a=1", true},
		{"wildcard catches multiple query", "/foo*", "/foo?a=1&b=2", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePattern(tc.pattern)
			if err != nil {
				t.Fatalf("ParsePattern(%q): %v", tc.pattern, err)
			}
			if got := p.Match(tc.path); got != tc.want {
				t.Errorf("Match(%q) on pattern %q = %v, want %v", tc.path, tc.pattern, got, tc.want)
			}
		})
	}
}
