package ldapclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
)

// ErrNotFound indicates that the requested LDAP entry does not exist.
var ErrNotFound = errors.New("ldap entry not found")

// Config encapsulates connection parameters for the LDAP server.
type Config struct {
	URL            string
	BindDN         string
	BindPassword   string
	UseStartTLS    bool
	InsecureTLS    bool
	DefaultTimeout time.Duration
}

// Client provides a thin, concurrency-safe wrapper around an LDAP connection.
type Client struct {
	cfg  Config
	conn *ldap.Conn
	mu   sync.Mutex
}

// New establishes a new LDAP client using the provided configuration.
func New(cfg Config) (*Client, error) {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}

	conn, err := dial(cfg)
	if err != nil {
		return nil, fmt.Errorf("dial ldap: %w", err)
	}

	client := &Client{cfg: cfg, conn: conn}

	if err := client.bind(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bind ldap: %w", err)
	}

	return client, nil
}

// Close terminates the underlying LDAP connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	return err
}

// SearchScope represents the scope of an LDAP search operation.
type SearchScope string

// Supported search scopes.
const (
	ScopeBaseObject   SearchScope = "base"
	ScopeSingleLevel  SearchScope = "one"
	ScopeWholeSubtree SearchScope = "sub"
)

func (s SearchScope) toLDAPScope() int {
	switch s {
	case ScopeBaseObject:
		return ldap.ScopeBaseObject
	case ScopeSingleLevel:
		return ldap.ScopeSingleLevel
	default:
		return ldap.ScopeWholeSubtree
	}
}

// DerefAliasesStrategy controls how the server dereferences aliases during a search.
type DerefAliasesStrategy string

// Supported dereference strategies.
const (
	DerefNever       DerefAliasesStrategy = "never"
	DerefInSearching DerefAliasesStrategy = "searching"
	DerefFindingBase DerefAliasesStrategy = "finding"
	DerefAlways      DerefAliasesStrategy = "always"
)

func (d DerefAliasesStrategy) toLDAP() int {
	switch d {
	case DerefInSearching:
		return ldap.DerefInSearching
	case DerefFindingBase:
		return ldap.DerefFindingBaseObj
	case DerefAlways:
		return ldap.DerefAlways
	default:
		return ldap.NeverDerefAliases
	}
}

// SearchRequest describes parameters for an LDAP search.
type SearchRequest struct {
	BaseDN       string
	Scope        SearchScope
	Filter       string
	Attributes   []string
	SizeLimit    int
	TypesOnly    bool
	DerefAliases DerefAliasesStrategy
	PageSize     uint32
}

// Entry represents an LDAP entry with its attributes.
type Entry struct {
	DN         string              `json:"dn"`
	Attributes map[string][]string `json:"attributes"`
}

// Search executes an LDAP search request.
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]Entry, error) {
	result := &ldap.SearchResult{}
	err := c.withConnection(ctx, func(conn *ldap.Conn) error {
		searchReq := ldap.NewSearchRequest(
			req.BaseDN,
			req.Scope.toLDAPScope(),
			req.DerefAliases.toLDAP(),
			req.SizeLimit,
			0,
			req.TypesOnly,
			req.Filter,
			req.Attributes,
			nil,
		)

		var err error
		if req.PageSize > 0 {
			result, err = conn.SearchWithPaging(searchReq, req.PageSize)
		} else {
			result, err = conn.Search(searchReq)
		}
		return err
	})
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	entries := make([]Entry, 0, len(result.Entries))
	for _, e := range result.Entries {
		entries = append(entries, convertEntry(e))
	}

	return entries, nil
}

// GetEntry fetches a single entry by DN.
func (c *Client) GetEntry(ctx context.Context, dn string, attributes []string) (*Entry, error) {
	entries, err := c.Search(ctx, SearchRequest{
		BaseDN:     dn,
		Scope:      ScopeBaseObject,
		Filter:     "(objectClass=*)",
		Attributes: attributes,
		SizeLimit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, ErrNotFound
	}
	return &entries[0], nil
}

// AddEntry creates a new LDAP entry.
func (c *Client) AddEntry(ctx context.Context, entry Entry) error {
	return c.withConnection(ctx, func(conn *ldap.Conn) error {
		addReq := ldap.NewAddRequest(entry.DN, nil)
		for attr, values := range entry.Attributes {
			addReq.Attribute(attr, values)
		}
		return conn.Add(addReq)
	})
}

// ModifyOperation represents a single modify action.
type ModifyOperation string

// Supported modify operations.
const (
	ModifyAdd     ModifyOperation = "add"
	ModifyReplace ModifyOperation = "replace"
	ModifyDelete  ModifyOperation = "delete"
)

// Modification captures an LDAP attribute change.
type Modification struct {
	Operation ModifyOperation
	Attribute string
	Values    []string
}

// ModifyEntry applies the requested modifications to an LDAP entry.
func (c *Client) ModifyEntry(ctx context.Context, dn string, mods []Modification) error {
	return c.withConnection(ctx, func(conn *ldap.Conn) error {
		modifyReq := ldap.NewModifyRequest(dn, nil)
		for _, mod := range mods {
			attribute := strings.TrimSpace(mod.Attribute)
			if attribute == "" {
				return fmt.Errorf("modify operation attribute cannot be empty")
			}
			switch mod.Operation {
			case ModifyAdd:
				if len(mod.Values) == 0 {
					return fmt.Errorf("add operation for %s requires at least one value", attribute)
				}
				modifyReq.Add(attribute, mod.Values)
			case ModifyReplace:
				if len(mod.Values) == 0 {
					return fmt.Errorf("replace operation for %s requires at least one value", attribute)
				}
				modifyReq.Replace(attribute, mod.Values)
			case ModifyDelete:
				modifyReq.Delete(attribute, mod.Values)
			default:
				return fmt.Errorf("unsupported modify operation: %s", mod.Operation)
			}
		}
		return conn.Modify(modifyReq)
	})
}

// DeleteEntry removes the LDAP entry identified by the provided DN.
func (c *Client) DeleteEntry(ctx context.Context, dn string) error {
	return c.withConnection(ctx, func(conn *ldap.Conn) error {
		delReq := ldap.NewDelRequest(dn, nil)
		return conn.Del(delReq)
	})
}

// ReadRootDSE retrieves the server's Root DSE entry.
func (c *Client) ReadRootDSE(ctx context.Context, attributes []string) (*Entry, error) {
	entries, err := c.Search(ctx, SearchRequest{
		BaseDN:     "",
		Scope:      ScopeBaseObject,
		Filter:     "(objectClass=*)",
		Attributes: attributes,
		SizeLimit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, ErrNotFound
	}
	return &entries[0], nil
}

func (c *Client) withConnection(ctx context.Context, fn func(*ldap.Conn) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	c.conn.SetTimeout(c.requestTimeout(ctx))

	err := fn(c.conn)
	if err == nil {
		return nil
	}

	if !needsReconnect(err) {
		return err
	}

	if reconnectErr := c.reconnectLocked(); reconnectErr != nil {
		return reconnectErr
	}

	c.conn.SetTimeout(c.requestTimeout(ctx))
	return fn(c.conn)
}

func (c *Client) requestTimeout(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if ok {
		timeout := time.Until(deadline)
		if timeout <= 0 {
			return time.Millisecond
		}
		if c.cfg.DefaultTimeout > 0 && timeout > c.cfg.DefaultTimeout {
			return c.cfg.DefaultTimeout
		}
		return timeout
	}
	if c.cfg.DefaultTimeout <= 0 {
		return 30 * time.Second
	}
	return c.cfg.DefaultTimeout
}

func (c *Client) reconnectLocked() error {
	if c.conn != nil {
		_ = c.conn.Close()
	}

	conn, err := dial(c.cfg)
	if err != nil {
		return fmt.Errorf("reconnect ldap: %w", err)
	}

	c.conn = conn
	return c.bind()
}

func (c *Client) bind() error {
	if c.cfg.BindDN == "" && c.cfg.BindPassword == "" {
		return c.conn.UnauthenticatedBind("")
	}
	return c.conn.Bind(c.cfg.BindDN, c.cfg.BindPassword)
}

func dial(cfg Config) (*ldap.Conn, error) {
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	dialer := &net.Dialer{Timeout: cfg.DefaultTimeout}

	var opts []ldap.DialOpt
	opts = append(opts, ldap.DialWithDialer(dialer))

	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}
	if host := parsed.Hostname(); host != "" {
		tlsConfig.ServerName = host
	}

	if parsed.Scheme == "ldaps" {
		opts = append(opts, ldap.DialWithTLSConfig(tlsConfig))
	}

	conn, err := ldap.DialURL(cfg.URL, opts...)
	if err != nil {
		return nil, err
	}

	if cfg.UseStartTLS {
		if parsed.Scheme == "ldaps" {
			_ = conn.Close()
			return nil, fmt.Errorf("starttls requested with ldaps scheme; use ldap:// for StartTLS")
		}
		if err := conn.StartTLS(tlsConfig); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}

	return conn, nil
}

func needsReconnect(err error) bool {
	return ldap.IsErrorAnyOf(err, ldap.ErrorNetwork, ldap.LDAPResultServerDown, ldap.LDAPResultTimeout, ldap.LDAPResultConnectError)
}

func convertEntry(entry *ldap.Entry) Entry {
	attributes := make(map[string][]string, len(entry.Attributes))
	for _, attr := range entry.Attributes {
		values := make([]string, len(attr.Values))
		copy(values, attr.Values)
		attributes[attr.Name] = values
	}

	return Entry{
		DN:         entry.DN,
		Attributes: attributes,
	}
}
