package cartridge

import (
	"fmt"
	"html/template"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jinzhu/inflection"
)

// defaultViewFuncs are the template functions every HTMLViews has:
//
//	{{timeAgo .CreatedAt}}           "5 minutes ago", "in 3 days", "just now"
//	{{pluralize .Count "reply"}}     "1 reply", "2 replies"
//	{{pluralize .Count "person" "people"}}
//	{{truncate 140 .Body}}           at most 140 characters, "…" at the cut
//	{{squish .Body}}                 one space for each run of white space
//	{{render "notes/row" (dict "Note" . "Compact" true)}}
//
// A function from WithTemplateFuncs, or from the funcs of NewHTMLViews,
// wins over one of these with the same name.
func defaultViewFuncs() template.FuncMap {
	return template.FuncMap{
		"timeAgo":   timeAgo,
		"pluralize": pluralize,
		"truncate":  truncate,
		"squish":    squish,
		"dict":      dict,
	}
}

// timeAgo says how long ago t was, or how long until it is, in the largest
// whole unit: "just now", "5 minutes ago", "in 3 days", "2 years ago". A
// month is 30 days and a year is 365 days. A zero or nil time gives "".
func timeAgo(value any) (string, error) {
	var t time.Time
	switch v := value.(type) {
	case time.Time:
		t = v
	case *time.Time:
		if v != nil {
			t = *v
		}
	default:
		return "", fmt.Errorf("timeAgo: %T is not a time.Time", value)
	}
	if t.IsZero() {
		return "", nil
	}

	d := time.Since(t)
	future := d < 0
	if future {
		d = -d
	}
	const day = 24 * time.Hour
	var n int64
	var unit string
	switch {
	case d < time.Minute:
		return "just now", nil
	case d < time.Hour:
		n, unit = int64(d/time.Minute), "minute"
	case d < day:
		n, unit = int64(d/time.Hour), "hour"
	case d < 30*day:
		n, unit = int64(d/day), "day"
	case d < 365*day:
		n, unit = int64(d/(30*day)), "month"
	default:
		n, unit = int64(d/(365*day)), "year"
	}
	phrase := countWord(n, unit, unit+"s")
	if future {
		return "in " + phrase, nil
	}
	return phrase + " ago", nil
}

// pluralize writes the count and the word, plural unless the count is 1:
// "1 reply", "0 replies". Without a plural, it makes the English plural of
// the last word of singular.
func pluralize(count any, singular string, plural ...string) (string, error) {
	n, err := toInt64(count)
	if err != nil {
		return "", fmt.Errorf("pluralize: %w", err)
	}
	many := inflection.Plural(singular)
	if len(plural) > 0 {
		many = plural[0]
	}
	return countWord(n, singular, many), nil
}

func countWord(n int64, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.FormatInt(n, 10) + " " + plural
}

// toInt64 reads any integer type, as counts from GORM are int64.
func toInt64(value any) (int64, error) {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint()), nil
	}
	return 0, fmt.Errorf("%T is not an integer", value)
}

// truncate shortens s to at most n characters. When it cuts, the last
// character is "…".
func truncate(n int, s string) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// squish removes white space at both ends of s and puts one space for each
// run of white space inside it.
func squish(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// dict makes a map from key and value pairs, to give a template more than
// one value.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: %d arguments, want key and value pairs", len(pairs))
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is a %T, want a string", pairs[i], pairs[i])
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}
