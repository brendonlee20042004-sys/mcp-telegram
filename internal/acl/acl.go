package acl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Prgebish/mcp-telegram/internal/config"
)

type PeerKind int

const (
	KindUser PeerKind = iota
	KindChat
	KindChannel
)

type PeerIdentity struct {
	Kind     PeerKind
	ID       int64
	Username string // without @
	Phone    string // with + prefix
}

type compiledRule struct {
	matcher func(PeerIdentity) bool
	perms   map[config.Permission]bool
	deny    bool
}

type Checker struct {
	rules []compiledRule
}

func NewChecker(cfg config.ACLConfig) (*Checker, error) {
	rules := make([]compiledRule, 0, len(cfg.Chats))
	for i, chat := range cfg.Chats {
		matcher, err := compileMatcher(chat.Match)
		if err != nil {
			return nil, fmt.Errorf("acl.chats[%d]: %w", i, err)
		}
		perms := make(map[config.Permission]bool, len(chat.Permissions))
		for _, p := range chat.Permissions {
			perms[p] = true
		}
		rules = append(rules, compiledRule{matcher: matcher, perms: perms, deny: chat.Deny})
	}
	return &Checker{rules: rules}, nil
}

// Allowed checks if peer has the given permission.
//
// Semantics:
//   - Allow rules grant the listed permissions; multiple matching allow rules
//     merge their permissions (no shadowing).
//   - Deny rules revoke the listed permissions on match. A deny rule overrides
//     any number of allow rules — explicit deny always wins.
//
// This lets a small ruleset express "@news_* can read, except @news_spam".
func (c *Checker) Allowed(peer PeerIdentity, perm config.Permission) bool {
	granted := false
	for _, rule := range c.rules {
		if !rule.matcher(peer) {
			continue
		}
		if !rule.perms[perm] {
			continue
		}
		if rule.deny {
			return false
		}
		granted = true
	}
	return granted
}

// MatchesAny reports whether any allow rule matches the peer. Deny-only
// matches don't count: a peer that exists solely to be denied shouldn't
// appear in dialog listings, search results, or anywhere else that uses
// MatchesAny as a visibility filter.
func (c *Checker) MatchesAny(peer PeerIdentity) bool {
	for _, rule := range c.rules {
		if rule.deny {
			continue
		}
		if rule.matcher(peer) {
			return true
		}
	}
	return false
}

// normalizePhone strips everything except + and digits.
func normalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r == '+' || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func compileMatcher(match string) (func(PeerIdentity) bool, error) {
	switch {
	case strings.HasPrefix(match, "@"):
		username := strings.ToLower(match[1:])
		return func(p PeerIdentity) bool {
			return strings.EqualFold(p.Username, username)
		}, nil

	case strings.HasPrefix(match, "+"):
		normalized := normalizePhone(match)
		return func(p PeerIdentity) bool {
			return normalizePhone(p.Phone) == normalized
		}, nil

	case strings.HasPrefix(match, "user:"):
		id, err := strconv.ParseInt(match[5:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid user ID in %q: %w", match, err)
		}
		return func(p PeerIdentity) bool {
			return p.Kind == KindUser && p.ID == id
		}, nil

	case strings.HasPrefix(match, "chat:"):
		id, err := strconv.ParseInt(match[5:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chat ID in %q: %w", match, err)
		}
		return func(p PeerIdentity) bool {
			return p.Kind == KindChat && p.ID == id
		}, nil

	case strings.HasPrefix(match, "channel:"):
		id, err := strconv.ParseInt(match[8:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid channel ID in %q: %w", match, err)
		}
		return func(p PeerIdentity) bool {
			return p.Kind == KindChannel && p.ID == id
		}, nil

	default:
		return nil, fmt.Errorf("unknown match format %q", match)
	}
}
