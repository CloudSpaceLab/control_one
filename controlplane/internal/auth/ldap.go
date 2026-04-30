// Package auth — LDAP bind-on-login provider.
//
// Each login attempt opens a fresh connection to the configured LDAP
// server, binds with a service-account search DN, looks up the user's
// DN by email/uid, then re-binds as the user with the supplied password.
// On success the user's group memberships are read and mapped to Control
// One roles via GroupRoleMap.
//
// Supports Active Directory and OpenLDAP via simple bind. SASL/Kerberos
// is out of scope for v1.

package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"go.uber.org/zap"
)

// LDAPConfig is loaded from the controlplane YAML / env. URL takes the
// `ldap://` or `ldaps://` form. BindDN + BindPassword are the service
// account used to search for user DNs.
type LDAPConfig struct {
	Enabled      bool              `mapstructure:"enabled"`
	URL          string            `mapstructure:"url"`
	StartTLS     bool              `mapstructure:"start_tls"`
	SkipVerify   bool              `mapstructure:"skip_verify"`
	BindDN       string            `mapstructure:"bind_dn"`
	BindPassword string            `mapstructure:"bind_password"`
	UserBaseDN   string            `mapstructure:"user_base_dn"`
	UserFilter   string            `mapstructure:"user_filter"` // default: (|(mail=%s)(uid=%s))
	GroupBaseDN  string            `mapstructure:"group_base_dn"`
	GroupFilter  string            `mapstructure:"group_filter"`   // default: (member=%s)
	GroupAttr    string            `mapstructure:"group_attr"`     // default: cn
	EmailAttr    string            `mapstructure:"email_attr"`     // default: mail
	NameAttr     string            `mapstructure:"name_attr"`      // default: displayName
	GroupRoleMap map[string]string `mapstructure:"group_role_map"` // ldap_group_name -> control_one_role
	DefaultRole  string            `mapstructure:"default_role"`   // when no group matches; default "viewer"
	Timeout      time.Duration     `mapstructure:"timeout"`
}

// LDAPProvider authenticates users against an LDAP directory. Stateless +
// safe to share across goroutines.
type LDAPProvider struct {
	cfg LDAPConfig
	log *zap.Logger
}

// NewLDAPProvider constructs the provider. Returns nil + nil when LDAP is
// disabled in config — the caller treats nil as "no LDAP path".
func NewLDAPProvider(cfg LDAPConfig, log *zap.Logger) (*LDAPProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.URL == "" {
		return nil, errors.New("ldap.url required when enabled")
	}
	if cfg.UserBaseDN == "" {
		return nil, errors.New("ldap.user_base_dn required")
	}
	if cfg.UserFilter == "" {
		cfg.UserFilter = "(|(mail=%s)(uid=%s)(sAMAccountName=%s))"
	}
	if cfg.GroupFilter == "" {
		cfg.GroupFilter = "(member=%s)"
	}
	if cfg.GroupAttr == "" {
		cfg.GroupAttr = "cn"
	}
	if cfg.EmailAttr == "" {
		cfg.EmailAttr = "mail"
	}
	if cfg.NameAttr == "" {
		cfg.NameAttr = "displayName"
	}
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = "viewer"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &LDAPProvider{cfg: cfg, log: log.Named("ldap")}, nil
}

// LDAPUser is the result of a successful authentication. Email + DN are
// always populated; Groups is the raw set the directory returned, Roles is
// the post-mapping set used by RBAC.
type LDAPUser struct {
	DN          string
	Email       string
	DisplayName string
	Groups      []string
	Roles       []string
}

// Authenticate runs the full bind-on-login flow:
//  1. service-bind with BindDN/BindPassword
//  2. search UserBaseDN for the supplied login (email or uid)
//  3. re-bind as the user's DN with supplied password
//  4. search GroupBaseDN for memberships
//  5. map groups → Control One roles via GroupRoleMap
func (p *LDAPProvider) Authenticate(ctx context.Context, login, password string) (*LDAPUser, error) {
	if password == "" {
		return nil, errors.New("password required")
	}
	conn, err := p.dial()
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	defer conn.Close()
	conn.SetTimeout(p.cfg.Timeout)

	if p.cfg.BindDN != "" {
		if err := conn.Bind(p.cfg.BindDN, p.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("ldap service bind: %w", err)
		}
	}

	// Find the user by login. Substitute %s in UserFilter for each %s slot.
	filter := substituteAll(p.cfg.UserFilter, ldap.EscapeFilter(login))
	res, err := conn.Search(ldap.NewSearchRequest(
		p.cfg.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		2, int(p.cfg.Timeout/time.Second), false,
		filter,
		[]string{"dn", p.cfg.EmailAttr, p.cfg.NameAttr},
		nil,
	))
	if err != nil {
		return nil, fmt.Errorf("ldap user search: %w", err)
	}
	if len(res.Entries) == 0 {
		return nil, ErrLDAPUnknownUser
	}
	if len(res.Entries) > 1 {
		// Ambiguous match — refuse rather than guess.
		return nil, errors.New("ldap: multiple entries match login")
	}
	entry := res.Entries[0]

	// Re-bind as the user's DN. This is the actual password verification.
	if err := conn.Bind(entry.DN, password); err != nil {
		// Generic error — caller maps to 401 + audit log records the
		// distinction internally.
		return nil, ErrLDAPInvalidCredentials
	}

	user := &LDAPUser{
		DN:          entry.DN,
		Email:       entry.GetAttributeValue(p.cfg.EmailAttr),
		DisplayName: entry.GetAttributeValue(p.cfg.NameAttr),
	}
	if user.Email == "" {
		user.Email = login
	}

	// Group lookup. Optional: when GroupBaseDN is empty we just attach
	// the default role.
	if p.cfg.GroupBaseDN != "" {
		groupFilter := substituteAll(p.cfg.GroupFilter, ldap.EscapeFilter(entry.DN))
		gres, err := conn.Search(ldap.NewSearchRequest(
			p.cfg.GroupBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
			0, int(p.cfg.Timeout/time.Second), false,
			groupFilter,
			[]string{p.cfg.GroupAttr},
			nil,
		))
		if err != nil {
			p.log.Warn("ldap group search failed; using default role", zap.Error(err))
		} else {
			for _, g := range gres.Entries {
				name := g.GetAttributeValue(p.cfg.GroupAttr)
				if name != "" {
					user.Groups = append(user.Groups, name)
				}
			}
		}
	}

	user.Roles = p.mapGroupsToRoles(user.Groups)
	if len(user.Roles) == 0 {
		user.Roles = []string{p.cfg.DefaultRole}
	}
	return user, nil
}

func (p *LDAPProvider) mapGroupsToRoles(groups []string) []string {
	if len(p.cfg.GroupRoleMap) == 0 || len(groups) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		// Case-insensitive match — AD groups often differ in case.
		for k, v := range p.cfg.GroupRoleMap {
			if strings.EqualFold(k, g) {
				if _, ok := seen[v]; !ok {
					out = append(out, v)
					seen[v] = struct{}{}
				}
			}
		}
	}
	return out
}

func (p *LDAPProvider) dial() (*ldap.Conn, error) {
	tlsCfg := &tls.Config{InsecureSkipVerify: p.cfg.SkipVerify}
	conn, err := ldap.DialURL(p.cfg.URL, ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, err
	}
	if p.cfg.StartTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	return conn, nil
}

// substituteAll replaces every %s in a filter template with the same
// value. ldap.EscapeFilter has already been applied.
func substituteAll(template, value string) string {
	out := template
	for strings.Contains(out, "%s") {
		out = strings.Replace(out, "%s", value, 1)
	}
	return out
}

// Sentinel errors. Caller maps both to HTTP 401.
var (
	ErrLDAPUnknownUser        = errors.New("ldap: unknown user")
	ErrLDAPInvalidCredentials = errors.New("ldap: invalid credentials")
)
