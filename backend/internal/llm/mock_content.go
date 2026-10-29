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
		raw, _ = json.Marshal(map[string]any{"messages": []content.Message{{Channel: "sms", Body: strings.Repeat("x", 170)}}})
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
	en := containsAny(b, []string{"english", "ingilizce", " the ", "customer", "discount"})
	formal := containsAny(b, []string{"resmi", "formal", "kurumsal"})
	cart := containsAny(b, cartWords)
	bought := containsAny(b, buyWords) && !negation.MatchString(b)

	offer := ""
	if p := pctRe.FindStringSubmatch(req.Brief); p != nil {
		n := p[1] + p[2]
		offer = map[bool]string{true: n + "% off", false: "%" + n + " indirim"}[en]
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

	var out []content.Message
	for _, ch := range req.Channels {
		switch ch {
		case "sms":
			out = append(out, content.Message{Channel: "sms", Body: fmt.Sprintf("%s, %s! %s", greet, theme, cta)})
		case "email":
			out = append(out, content.Message{Channel: "email", Subject: subject,
				Body: fmt.Sprintf("%s,\n\n%s.\n\n%s", greet, strings.ToUpper(theme[:1])+theme[1:], cta)})
		}
	}
	return out
}
