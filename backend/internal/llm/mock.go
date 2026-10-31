package llm

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/praganter/insider-case/backend/internal/segment"
)

// Mock is the key-less mode: it reads a few patterns out of the brief and
// builds a valid rule from them. BadFirst and Err exist for tests.
type Mock struct {
	BadFirst bool          // first answer is invalid, the repair answer is valid
	Err      error         // returned instead of an answer
	Delay    time.Duration // pause between streamed pieces of content
}

func (Mock) Mode() string { return "mock" }

var mockCities = []string{"İstanbul", "Ankara", "İzmir", "Bursa", "Antalya", "Adana", "Konya", "Gaziantep", "Eskişehir", "Trabzon"}

var (
	daysRe    = regexp.MustCompile(`(\d+)\s*(gün|gun|day)`)
	negation  = regexp.MustCompile(`almayan|almamis|yapmayan|yapmamis|not |without|didn't|haven't|never`)
	cartWords = []string{"sepet", "cart"}
	buyWords  = []string{"satin al", "alisveris", "purchase", "bought", "buy"}

	// Turkish words take suffixes ("erkeklere"), so they match as prefixes; English ones as whole words.
	femaleRe = regexp.MustCompile(`\b(?:kadin|bayan|women\b|woman\b|female\b|ladies\b)`)
	maleRe   = regexp.MustCompile(`\b(?:erkek|men\b|man\b|male\b)`)
	// Every age form needs an age word, so "%30 indirim" or "30 gun" never match.
	ageRangeRe = regexp.MustCompile(`(\d+)\s*(?:-|ile|to)\s*(\d+)\s*(?:yas|years? old)`)
	ageMinRe   = regexp.MustCompile(`(\d+)\s*yas\s*(?:ustu|uzeri)|over\s+(\d+)\s*years? old|(\d+)\+\s*(?:yas|years? old)`)
	ageMaxRe   = regexp.MustCompile(`(\d+)\s*yas\s*alti|under\s+(\d+)\s*years? old`)
)

// fold lowercases and strips Turkish letters so "İstanbul'daki" matches "istanbul".
var fold = strings.NewReplacer("İ", "i", "I", "i", "ı", "i", "Ş", "s", "ş", "s", "Ğ", "g", "ğ", "g",
	"Ü", "u", "ü", "u", "Ö", "o", "ö", "o", "Ç", "c", "ç", "c")

func norm(s string) string { return strings.ToLower(fold.Replace(s)) }

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// firstNumber returns the first captured number of a match, since the Turkish
// and English forms of a pattern capture into different groups.
func firstNumber(m []string) string {
	for _, s := range m[min(1, len(m)):] {
		if s != "" {
			return s
		}
	}
	return ""
}

func ageCond(op, n string) segment.Node {
	v, _ := strconv.Atoi(n)
	return segment.Node{Field: "age", Op: op, Value: v}
}

func (m Mock) Audience(_ context.Context, brief string, previous json.RawMessage, _ []string) (json.RawMessage, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.BadFirst && previous == nil {
		return json.RawMessage(`{"match":"all","conditions":[{"field":"password","op":"eq","value":"x"}]}`), nil
	}
	b := norm(brief)
	days := 30
	if m := daysRe.FindStringSubmatch(b); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 1 && n <= segment.MaxWithinDays {
			days = n
		}
	}
	yes, no := true, false
	var conds []segment.Node
	for _, c := range mockCities {
		if strings.Contains(b, norm(c)) {
			conds = append(conds, segment.Node{Field: "city", Op: "eq", Value: c})
			break
		}
	}
	if strings.Contains(b, "ingilizce") || strings.Contains(b, "english") {
		conds = append(conds, segment.Node{Field: "language", Op: "eq", Value: "en"})
	}
	// Both genders in one brief is ambiguous, so neither is used.
	if female, male := femaleRe.MatchString(b), maleRe.MatchString(b); female != male {
		gender := "male"
		if female {
			gender = "female"
		}
		conds = append(conds, segment.Node{Field: "gender", Op: "eq", Value: gender})
	}
	if m := ageRangeRe.FindStringSubmatch(b); m != nil {
		conds = append(conds, ageCond("gte", m[1]), ageCond("lte", m[2]))
	} else if n := firstNumber(ageMinRe.FindStringSubmatch(b)); n != "" {
		conds = append(conds, ageCond("gte", n))
	} else if n := firstNumber(ageMaxRe.FindStringSubmatch(b)); n != "" {
		conds = append(conds, ageCond("lt", n))
	}
	if containsAny(b, cartWords) {
		conds = append(conds, segment.Node{Event: "cart_add", WithinDays: days, Exists: &yes})
	}
	if containsAny(b, buyWords) {
		exists := &yes
		if negation.MatchString(b) {
			exists = &no
		}
		conds = append(conds, segment.Node{Event: "purchase", WithinDays: days, Exists: exists})
	}
	if len(conds) == 0 {
		// Unrecognized brief: recently active users is the safest broad audience.
		conds = []segment.Node{{Event: "page_view", WithinDays: days, Exists: &yes}}
	}
	return json.Marshal(segment.Node{Match: "all", Conditions: conds})
}
