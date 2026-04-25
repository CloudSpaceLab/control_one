// Package aclenforcer evaluates command_acl rules on the node side. It is
// intentionally small: the control plane publishes ACL rows; the agent caches
// them; this package answers "is this command permitted for this role on this
// node?". Plumbing the decision into the shell (bash PROMPT_COMMAND hook or
// auditd) is left to the caller — this package is the pure decision engine.
package aclenforcer

import (
	"regexp"
	"strings"
	"sync"
)

// Rule mirrors the control plane command_acl row the agent cares about.
type Rule struct {
	ID             string
	Name           string
	Role           string
	NodeLabels     map[string]string
	AllowCommands  []string // literal or regex with /.../ wrapper
	DenyCommands   []string
	compiledAllow  []*regexp.Regexp
	compiledDeny   []*regexp.Regexp
}

// Decision describes the outcome of evaluating a command against the ACL set.
type Decision struct {
	Allowed  bool
	RuleID   string
	Reason   string
}

// Enforcer holds the current rules and answers Evaluate.
type Enforcer struct {
	mu         sync.RWMutex
	rules      []Rule
	nodeLabels map[string]string
	denyFirst  bool
}

// New returns an empty enforcer. Call ReplaceRules after bootstrap once the
// agent has pulled the current ACL set from the control plane.
func New(nodeLabels map[string]string) *Enforcer {
	return &Enforcer{nodeLabels: nodeLabels, denyFirst: true}
}

// ReplaceRules atomically swaps in a new ruleset. Patterns are compiled up
// front so Evaluate stays cheap per command.
func (e *Enforcer) ReplaceRules(rules []Rule) {
	compiled := make([]Rule, len(rules))
	for i, r := range rules {
		r.compiledAllow = compilePatterns(r.AllowCommands)
		r.compiledDeny = compilePatterns(r.DenyCommands)
		compiled[i] = r
	}
	e.mu.Lock()
	e.rules = compiled
	e.mu.Unlock()
}

// Evaluate returns a Decision for a given role/command. Semantics:
//
//	1. Iterate all rules whose role and node_label_selector match.
//	2. If any rule's deny list matches, the command is blocked (deny-first).
//	3. If any rule's allow list matches, the command is permitted.
//	4. Default-permit when no rule references the command — this mirrors sudo
//	   behavior and avoids locking operators out when ACLs are sparse.
func (e *Enforcer) Evaluate(role, command string) Decision {
	if command == "" {
		return Decision{Allowed: true, Reason: "empty"}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, r := range e.rules {
		if !e.ruleAppliesToRole(r, role) {
			continue
		}
		if e.denyFirst {
			for _, re := range r.compiledDeny {
				if re.MatchString(command) {
					return Decision{Allowed: false, RuleID: r.ID, Reason: "deny: " + r.Name}
				}
			}
		}
		for _, re := range r.compiledAllow {
			if re.MatchString(command) {
				return Decision{Allowed: true, RuleID: r.ID, Reason: "allow: " + r.Name}
			}
		}
	}
	return Decision{Allowed: true, Reason: "default"}
}

func (e *Enforcer) ruleAppliesToRole(r Rule, role string) bool {
	if r.Role != "" && r.Role != role {
		return false
	}
	if len(r.NodeLabels) > 0 {
		for k, v := range r.NodeLabels {
			if got := e.nodeLabels[k]; got != v {
				return false
			}
		}
	}
	return true
}

// compilePatterns accepts either literal command prefixes or /regex/ forms.
// A literal "rm -rf" matches when the command starts with "rm -rf".
// A regex "/docker.*/" matches anywhere in the command.
func compilePatterns(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var re *regexp.Regexp
		var err error
		if strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/") && len(p) > 2 {
			re, err = regexp.Compile(p[1 : len(p)-1])
		} else {
			re, err = regexp.Compile("^" + regexp.QuoteMeta(p))
		}
		if err != nil {
			continue
		}
		out = append(out, re)
	}
	return out
}
