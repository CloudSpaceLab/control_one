package ldap

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"go.uber.org/zap"
)

// Client provides access to LDAP/Active Directory.
type Client struct {
	conn   *ldap.Conn
	config Config
	log    *zap.Logger
}

// Config configures the LDAP client.
type Config struct {
	Server       string
	Port         int
	BaseDN       string
	BindDN       string
	BindPassword string
	UseTLS       bool
	SkipVerify   bool
	Timeout      time.Duration
}

// NewClient creates a new LDAP client.
func NewClient(log *zap.Logger, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Server) == "" {
		return nil, fmt.Errorf("ldap server is required")
	}
	if strings.TrimSpace(cfg.BaseDN) == "" {
		return nil, fmt.Errorf("base DN is required")
	}

	port := cfg.Port
	if port == 0 {
		if cfg.UseTLS {
			port = 636
		} else {
			port = 389
		}
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	address := fmt.Sprintf("%s:%d", cfg.Server, port)
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := ldap.DialURL(fmt.Sprintf("ldap://%s", address), ldap.DialWithDialer(dialer))
	if err != nil {
		return nil, fmt.Errorf("connect to ldap server: %w", err)
	}

	if cfg.UseTLS {
		err = conn.StartTLS(&tls.Config{
			InsecureSkipVerify: cfg.SkipVerify,
		})
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("start tls: %w", err)
		}
	}

	client := &Client{
		conn:   conn,
		config: cfg,
		log:    log,
	}

	if cfg.BindDN != "" {
		if err := client.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			conn.Close()
			return nil, fmt.Errorf("bind to ldap: %w", err)
		}
	}

	return client, nil
}

// Bind authenticates to the LDAP server.
func (c *Client) Bind(dn, password string) error {
	return c.conn.Bind(dn, password)
}

// Close closes the LDAP connection.
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// SearchUsers searches for users in Active Directory.
func (c *Client) SearchUsers(filter string, attributes []string) ([]User, error) {
	if filter == "" {
		filter = "(objectClass=user)"
	}

	searchRequest := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		filter,
		attributes,
		nil,
	)

	sr, err := c.conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("ldap search failed: %w", err)
	}

	users := make([]User, 0, len(sr.Entries))
	for _, entry := range sr.Entries {
		user := User{
			DN: entry.DN,
		}

		for _, attr := range attributes {
			if values := entry.GetAttributeValues(attr); len(values) > 0 {
				switch strings.ToLower(attr) {
				case "samaccountname", "uid":
					user.Username = values[0]
				case "mail", "email":
					user.Email = values[0]
				case "cn", "displayname":
					user.DisplayName = values[0]
				case "memberof":
					user.Groups = values
				}
			}
		}

		users = append(users, user)
	}

	return users, nil
}

// SearchGroups searches for groups in Active Directory.
func (c *Client) SearchGroups(filter string) ([]Group, error) {
	if filter == "" {
		filter = "(objectClass=group)"
	}

	searchRequest := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		filter,
		[]string{"cn", "member", "memberOf", "description"},
		nil,
	)

	sr, err := c.conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("ldap search failed: %w", err)
	}

	groups := make([]Group, 0, len(sr.Entries))
	for _, entry := range sr.Entries {
		group := Group{
			DN:      entry.DN,
			Name:    entry.GetAttributeValue("cn"),
			Members: entry.GetAttributeValues("member"),
		}

		groups = append(groups, group)
	}

	return groups, nil
}

// User represents an LDAP user.
type User struct {
	DN          string
	Username    string
	Email       string
	DisplayName string
	Groups      []string
}

// Group represents an LDAP group.
type Group struct {
	DN      string
	Name    string
	Members []string
}
