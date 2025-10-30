package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/trxo/ldap-mcp/internal/ldapclient"
)

// ResourceSet exposes LDAP resources over MCP.
type ResourceSet struct {
	client *ldapclient.Client
}

// New creates a new ResourceSet.
func New(client *ldapclient.Client) *ResourceSet {
	return &ResourceSet{client: client}
}

// GetResources returns the static MCP resources.
func (r *ResourceSet) GetResources() []mcp.Resource {
	return []mcp.Resource{
		mcp.NewResource(
			"ldap://root-dse",
			"LDAP Root DSE",
			mcp.WithResourceDescription("Root DSE attributes for the LDAP server"),
			mcp.WithMIMEType("application/json"),
		),
	}
}

// GetResourceTemplates exposes templated resources for LDAP entries.
func (r *ResourceSet) GetResourceTemplates() []mcp.ResourceTemplate {
	return []mcp.ResourceTemplate{
		mcp.NewResourceTemplate(
			"ldap://entry/{dn}",
			"LDAP Entry",
			mcp.WithTemplateDescription("Retrieve an LDAP entry by DN (URL-escaped)"),
			mcp.WithTemplateMIMEType("application/json"),
		),
	}
}

// HandleResource resolves resource requests by querying LDAP.
func (r *ResourceSet) HandleResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	uri := request.Params.URI

	switch {
	case uri == "ldap://root-dse":
		return r.handleRootDSE(ctx)
	case strings.HasPrefix(uri, "ldap://entry/"):
		dn, err := decodeDN(strings.TrimPrefix(uri, "ldap://entry/"))
		if err != nil {
			return nil, err
		}
		return r.handleEntry(ctx, dn)
	default:
		return nil, fmt.Errorf("unknown resource URI: %s", uri)
	}
}

func (r *ResourceSet) handleRootDSE(ctx context.Context) ([]mcp.ResourceContents, error) {
	entry, err := r.client.ReadRootDSE(ctx, nil)
	if err != nil {
		return nil, err
	}

	payload, err := jsonPayload(map[string]any{"root_dse": entry})
	if err != nil {
		return nil, err
	}

	payload.URI = "ldap://root-dse"

	return []mcp.ResourceContents{payload}, nil
}

func (r *ResourceSet) handleEntry(ctx context.Context, dn string) ([]mcp.ResourceContents, error) {
	entry, err := r.client.GetEntry(ctx, dn, nil)
	if err != nil {
		return nil, err
	}

	payload, err := jsonPayload(map[string]any{"entry": entry})
	if err != nil {
		return nil, err
	}

	payload.URI = fmt.Sprintf("ldap://entry/%s", url.PathEscape(entry.DN))
	return []mcp.ResourceContents{payload}, nil
}

func decodeDN(encoded string) (string, error) {
	dn, err := url.PathUnescape(encoded)
	if err != nil {
		return "", fmt.Errorf("invalid DN encoding: %w", err)
	}
	if strings.TrimSpace(dn) == "" {
		return "", fmt.Errorf("dn cannot be empty")
	}
	return dn, nil
}

func jsonPayload(data map[string]any) (mcp.TextResourceContents, error) {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return mcp.TextResourceContents{}, fmt.Errorf("marshal resource: %w", err)
	}
	return mcp.TextResourceContents{
		URI:      "",
		MIMEType: "application/json",
		Text:     string(encoded),
	}, nil
}
