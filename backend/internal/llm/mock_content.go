package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/praganter/insider-case/backend/internal/content"
)

var pctRe = regexp.MustCompile(`%\s*(\d{1,2})|(\d{1,2})\s*%`)

// Content builds template copy from the brief (language, discount, cart or
// purchase theme, formal or friendly tone) and streams it in small pieces.
func (m Mock) Content(ctx context.Context, req ContentRequest, s Stream) (json.RawMessage, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var raw []byte
	if m.BadFirst && req.Previous == nil {
		// A valid shape whose SMS is over the limit: exercises the repair round.
		raw, _ = json.Marshal(map[string]any{"messages": []content.Message{{Channel: "sms", Variant: "A", Language: "tr", Body: strings.Repeat("x", 170)}}})
	} else {
		raw, _ = json.Marshal(map[string]any{"messages": mockMessages(req)})
	}
	rs := []rune(string(raw))
	for i := 0; i < len(rs); i += 12 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.Delay):
		}
		s.delta(string(rs[i:min(i+12, len(rs))]))
	}
	return raw, nil
}

func mockMessages(req ContentRequest) []content.Message {
	b := norm(req.Brief)
	formal := containsAny(b, []string{"resmi", "formal", "kurumsal"})
	cart := containsAny(b, cartWords)
	bought := containsAny(b, buyWords) && !negation.MatchString(b)
	pct := ""
	if p := pctRe.FindStringSubmatch(req.Brief); p != nil {
		pct = p[1] + p[2]
	}

	var out []content.Message
	for _, sl := range req.Slots {
		// The language comes from the slot, not from guessing at the brief.
		en := sl.Language == "en"
		offer := ""
		if pct != "" {
			offer = map[bool]string{true: pct + "% off", false: "%" + pct + " indirim"}[en]
		}

		var greet, theme, cta, subject string
		switch {
		case en && formal:
			greet = "Dear {{first_name}} {{last_name}}"
		case en:
			greet = "Hi {{first_name}}"
		case formal:
			greet = "Sayın {{first_name}} {{last_name}}"
		default:
			greet = "Merhaba {{first_name}}"
		}
		switch {
		case en && cart:
			theme, cta, subject = "your cart is waiting for you", "Complete your order now.", "Your cart is waiting, {{first_name}}"
		case en && bought:
			theme, cta, subject = "thank you for your purchase", "See what is new for you.", "Thank you, {{first_name}}"
		case en:
			theme, cta, subject = "we have something special for you", "Take a look today.", "Something special for you, {{first_name}}"
		case cart:
			theme, cta, subject = "sepetinde seni bekleyen ürünler var", "Siparişini hemen tamamla.", "Sepetin seni bekliyor, {{first_name}}"
		case bought:
			theme, cta, subject = "alışverişin için teşekkürler", "Yeni fırsatlara göz at.", "Teşekkürler {{first_name}}"
		default:
			theme, cta, subject = "sana özel bir fırsatımız var", "Hemen göz at.", "Sana özel fırsat, {{first_name}}"
		}
		if offer != "" {
			cta = strings.TrimSpace(strings.ToUpper(offer[:1]) + offer[1:] + ". " + cta)
		}
		// Variant B is the urgent take on the same offer.
		if sl.Variant == "B" {
			cta = map[bool]string{true: "Today only! ", false: "Sadece bugün! "}[en] + cta
			// The old subject now follows a comma, so its first letter drops to lower case.
			r := []rune(subject)
			subject = map[bool]string{true: "Last chance, ", false: "Son fırsat, "}[en] + strings.ToLower(string(r[:1])) + string(r[1:])
		}

		m := content.Message{Channel: sl.Channel, Variant: sl.Variant, Language: sl.Language}
		switch sl.Channel {
		case "sms":
			m.Body = fmt.Sprintf("%s, %s! %s", greet, theme, cta)
		case "email":
			m.Subject = subject
			m.Body = fmt.Sprintf("%s,\n\n%s.\n\n%s", greet, strings.ToUpper(theme[:1])+theme[1:], cta)
		}
		out = append(out, m)
	}
	return out
}
