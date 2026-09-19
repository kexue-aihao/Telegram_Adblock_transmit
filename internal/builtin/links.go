package builtin

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type linkSignals struct {
	any      bool
	telegram bool
	invite   bool
	joinchat bool
	short    bool
	bot      bool
}

func (l *linkSignals) merge(other linkSignals) {
	l.any = l.any || other.any
	l.telegram = l.telegram || other.telegram
	l.invite = l.invite || other.invite
	l.joinchat = l.joinchat || other.joinchat
	l.short = l.short || other.short
	l.bot = l.bot || other.bot
}

var urlCandidates = regexp.MustCompile(`(?i)(?:https?://|tg://)[^\s<>"'“”‘’，。！？；、]+|(?:[a-z0-9][a-z0-9-]*\.)+[a-z]{2,63}(?::[0-9]+)?(?:/[^\s<>"'“”‘’，。！？；、]*)?`)

var shortHosts = map[string]bool{
	"bit.ly": true, "goo.gl": true, "tinyurl.com": true, "rb.gy": true,
	"0rz.tw": true, "t.co": true, "is.gd": true, "ow.ly": true,
	"shorturl.at": true, "cutt.ly": true, "clck.ru": true, "rebrand.ly": true,
	"lnkd.in": true, "s.id": true, "u.to": true, "urlzs.com": true, "soo.gd": true,
}

func classifyURL(value string) linkSignals {
	value = strings.TrimSpace(normalize(value))
	value = strings.TrimRight(value, ".,;:!?)]}。，；：！？）】》")
	if value == "" {
		return linkSignals{}
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return linkSignals{}
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	switch parsed.Scheme {
	case "tg":
		if host == "join" && parsed.Query().Get("invite") != "" {
			return linkSignals{any: true, telegram: true, invite: true}
		}
		if host == "resolve" && parsed.Query().Get("domain") != "" {
			return linkSignals{any: true, telegram: true, bot: strings.HasSuffix(strings.ToLower(parsed.Query().Get("domain")), "bot")}
		}
		if host == "user" && parsed.Query().Get("id") != "" {
			return linkSignals{any: true, telegram: true}
		}
		return linkSignals{}
	case "https", "http":
	default:
		return linkSignals{}
	}
	if strings.ContainsAny(host, " \t\r\n/@") {
		return linkSignals{}
	}
	result := linkSignals{any: true}
	path := strings.TrimPrefix(parsed.Path, "/")
	switch host {
	case "t.me", "www.t.me", "telegram.me", "www.telegram.me", "telegram.dog", "www.telegram.dog":
		result.telegram = true
		result.invite = strings.HasPrefix(path, "+") && len(path) > 1
		result.joinchat = strings.HasPrefix(path, "joinchat/") && len(path) > len("joinchat/")
		username := strings.SplitN(path, "/", 2)[0]
		result.bot = username != "" && strings.HasSuffix(username, "bot")
	default:
		result.short = shortHosts[host] && path != ""
	}
	return result
}

func textLinks(text string) linkSignals {
	var result linkSignals
	for _, span := range urlCandidates.FindAllStringIndex(text, -1) {
		if span[0] > 0 {
			before, _ := utf8.DecodeLastRuneInString(text[:span[0]])
			if unicode.IsLetter(before) || unicode.IsDigit(before) || strings.ContainsRune("_@./", before) {
				continue
			}
		}
		result.merge(classifyURL(text[span[0]:span[1]]))
	}
	return result
}
