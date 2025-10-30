package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/trxo/ldap-mcp/internal/ldapclient"
)

// Toolset exposes LDAP operations as MCP tools.
type Toolset struct {
	client *ldapclient.Client
}

// New constructs a new Toolset.
func New(client *ldapclient.Client) *Toolset {
	return &Toolset{client: client}
}

// GetTools returns the list of registered MCP tools.
func (t *Toolset) GetTools(readWrite bool) []mcp.Tool {
	tools := []mcp.Tool{
		t.searchEntriesTool(),
		t.getEntryTool(),
	}

	if readWrite {
		tools = append(tools,
			t.addEntryTool(),
			t.modifyEntryTool(),
			t.deleteEntryTool(),
		)
	}

	return tools
}

// HandleTool routes MCP tool invocations to LDAP operations.
func (t *Toolset) HandleTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	switch request.Params.Name {
	case "search_entries":
		return t.handleSearchEntries(ctx, request)
	case "get_entry":
		return t.handleGetEntry(ctx, request)
	case "add_entry":
		return t.handleAddEntry(ctx, request)
	case "modify_entry":
		return t.handleModifyEntry(ctx, request)
	case "delete_entry":
		return t.handleDeleteEntry(ctx, request)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown tool: %s", request.Params.Name)), nil
	}
}

func (t *Toolset) searchEntriesTool() mcp.Tool {
	return mcp.NewTool(
		"search_entries",
		mcp.WithDescription("Execute an LDAP search operation"),
		mcp.WithString("base_dn", mcp.Required(), mcp.Description("Base distinguished name for the search")),
		mcp.WithString("filter", mcp.Required(), mcp.Description("LDAP search filter (RFC 4515)")),
		mcp.WithString("scope", mcp.Description("Search scope: base, one, or sub"), mcp.Enum("base", "one", "sub"), mcp.DefaultString("sub")),
		mcp.WithArray("attributes", mcp.Description("Attributes to return; empty array returns all"), mcp.WithStringItems()),
		mcp.WithNumber("size_limit", mcp.Description("Maximum number of entries to return (0 means server default)"), mcp.Min(0)),
		mcp.WithBoolean("types_only", mcp.Description("Return only attribute types without values")),
		mcp.WithNumber("page_size", mcp.Description("Optional Simple Paged Results size (0 disables paging)"), mcp.Min(0)),
		mcp.WithString("deref_aliases", mcp.Description("Alias dereferencing strategy: never, searching, finding, or always"), mcp.Enum("never", "searching", "finding", "always"), mcp.DefaultString("never")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "LDAP Search",
			ReadOnlyHint:    mcp.ToBoolPtr(true),
			DestructiveHint: mcp.ToBoolPtr(false),
			IdempotentHint:  mcp.ToBoolPtr(true),
			OpenWorldHint:   mcp.ToBoolPtr(true),
		}),
	)
}

func (t *Toolset) getEntryTool() mcp.Tool {
	return mcp.NewTool(
		"get_entry",
		mcp.WithDescription("Retrieve a single LDAP entry by DN"),
		mcp.WithString("dn", mcp.Required(), mcp.Description("The distinguished name of the entry")),
		mcp.WithArray("attributes", mcp.Description("Attributes to return; empty array returns all"), mcp.WithStringItems()),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get LDAP Entry",
			ReadOnlyHint:    mcp.ToBoolPtr(true),
			DestructiveHint: mcp.ToBoolPtr(false),
			IdempotentHint:  mcp.ToBoolPtr(true),
			OpenWorldHint:   mcp.ToBoolPtr(true),
		}),
	)
}

func (t *Toolset) addEntryTool() mcp.Tool {
	return mcp.NewTool(
		"add_entry",
		mcp.WithDescription("Create a new LDAP entry"),
		mcp.WithString("dn", mcp.Required(), mcp.Description("Distinguished name for the new entry")),
		mcp.WithObject(
			"attributes",
			mcp.Required(),
			mcp.Description("Map of attribute names to string arrays"),
			mcp.AdditionalProperties(map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			}),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Add LDAP Entry",
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(true),
			IdempotentHint:  mcp.ToBoolPtr(false),
			OpenWorldHint:   mcp.ToBoolPtr(true),
		}),
	)
}

func (t *Toolset) modifyEntryTool() mcp.Tool {
	return mcp.NewTool(
		"modify_entry",
		mcp.WithDescription("Modify attributes of an LDAP entry"),
		mcp.WithString("dn", mcp.Required(), mcp.Description("The distinguished name of the entry")),
		mcp.WithArray(
			"changes",
			mcp.Required(),
			mcp.Description("List of attribute modifications"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"enum":        []string{"add", "replace", "delete"},
						"description": "Modification operation",
					},
					"attribute": map[string]any{
						"type":        "string",
						"description": "Attribute name",
					},
					"values": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Attribute values (ignored for delete)",
					},
				},
				"required": []string{"operation", "attribute"},
			}),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Modify LDAP Entry",
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(true),
			IdempotentHint:  mcp.ToBoolPtr(false),
			OpenWorldHint:   mcp.ToBoolPtr(true),
		}),
	)
}

func (t *Toolset) deleteEntryTool() mcp.Tool {
	return mcp.NewTool(
		"delete_entry",
		mcp.WithDescription("Delete an LDAP entry by DN"),
		mcp.WithString("dn", mcp.Required(), mcp.Description("The distinguished name to delete")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Delete LDAP Entry",
			ReadOnlyHint:    mcp.ToBoolPtr(false),
			DestructiveHint: mcp.ToBoolPtr(true),
			IdempotentHint:  mcp.ToBoolPtr(false),
			OpenWorldHint:   mcp.ToBoolPtr(true),
		}),
	)
}

func (t *Toolset) handleSearchEntries(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	baseDN := mcp.ParseString(request, "base_dn", "")
	filter := mcp.ParseString(request, "filter", "")
	if strings.TrimSpace(baseDN) == "" {
		return mcp.NewToolResultError("base_dn parameter is required"), nil
	}
	if strings.TrimSpace(filter) == "" {
		return mcp.NewToolResultError("filter parameter is required"), nil
	}

	scope := ldapclient.ScopeWholeSubtree
	scopeStr := strings.ToLower(mcp.ParseString(request, "scope", "sub"))
	switch scopeStr {
	case "base":
		scope = ldapclient.ScopeBaseObject
	case "one":
		scope = ldapclient.ScopeSingleLevel
	case "sub":
		scope = ldapclient.ScopeWholeSubtree
	default:
		return mcp.NewToolResultError("scope must be one of base, one, or sub"), nil
	}

	attributes := parseStringSlice(request, "attributes")
	if len(attributes) == 0 {
		attributes = nil
	}

	sizeLimit := request.GetInt("size_limit", 0)
	if sizeLimit < 0 {
		sizeLimit = 0
	}

	pageSize := request.GetInt("page_size", 0)
	if pageSize < 0 {
		pageSize = 0
	}
	if pageSize > int(math.MaxUint32) {
		pageSize = int(math.MaxUint32)
	}

	typesOnly := request.GetBool("types_only", false)
	deref := ldapclient.DerefAliasesStrategy(strings.ToLower(mcp.ParseString(request, "deref_aliases", "never")))
	switch deref {
	case ldapclient.DerefNever, ldapclient.DerefInSearching, ldapclient.DerefFindingBase, ldapclient.DerefAlways:
		// valid
	default:
		return mcp.NewToolResultError("deref_aliases must be one of never, searching, finding, or always"), nil
	}

	entries, err := t.client.Search(ctx, ldapclient.SearchRequest{
		BaseDN:       baseDN,
		Scope:        scope,
		Filter:       filter,
		Attributes:   attributes,
		SizeLimit:    sizeLimit,
		TypesOnly:    typesOnly,
		DerefAliases: deref,
		PageSize:     uint32(pageSize),
	})
	if err != nil {
		return toolErrorFromLDAP(err), nil
	}

	payload := map[string]any{
		"entries": entries,
		"count":   len(entries),
	}

	return newJSONResult(payload, "LDAP search succeeded"), nil
}

func (t *Toolset) handleGetEntry(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dn := mcp.ParseString(request, "dn", "")
	if strings.TrimSpace(dn) == "" {
		return mcp.NewToolResultError("dn parameter is required"), nil
	}

	attributes := parseStringSlice(request, "attributes")
	if len(attributes) == 0 {
		attributes = nil
	}

	entry, err := t.client.GetEntry(ctx, dn, attributes)
	if err != nil {
		return toolErrorFromLDAP(err), nil
	}

	payload := map[string]any{
		"entry": entry,
	}

	return newJSONResult(payload, "LDAP entry retrieved"), nil
}

func (t *Toolset) handleAddEntry(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dn := mcp.ParseString(request, "dn", "")
	if strings.TrimSpace(dn) == "" {
		return mcp.NewToolResultError("dn parameter is required"), nil
	}

	attributesArg, ok := request.GetArguments()["attributes"].(map[string]any)
	if !ok || len(attributesArg) == 0 {
		return mcp.NewToolResultError("attributes must be an object mapping attribute names to arrays"), nil
	}

	attributes, err := decodeAttributes(attributesArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	err = t.client.AddEntry(ctx, ldapclient.Entry{DN: dn, Attributes: attributes})
	if err != nil {
		return toolErrorFromLDAP(err), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Entry %s created successfully", dn)), nil
}

func (t *Toolset) handleModifyEntry(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dn := mcp.ParseString(request, "dn", "")
	if strings.TrimSpace(dn) == "" {
		return mcp.NewToolResultError("dn parameter is required"), nil
	}

	rawChanges, ok := request.GetArguments()["changes"].([]any)
	if !ok || len(rawChanges) == 0 {
		return mcp.NewToolResultError("changes must be a non-empty array"), nil
	}

	mods := make([]ldapclient.Modification, 0, len(rawChanges))
	for i, raw := range rawChanges {
		change, ok := raw.(map[string]any)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("change at index %d must be an object", i)), nil
		}

		opStr, _ := change["operation"].(string)
		attr, _ := change["attribute"].(string)
		if strings.TrimSpace(opStr) == "" || strings.TrimSpace(attr) == "" {
			return mcp.NewToolResultError(fmt.Sprintf("change at index %d must include operation and attribute", i)), nil
		}

		values := make([]string, 0)
		if rawVals, ok := change["values"].([]any); ok {
			for idx, v := range rawVals {
				if str, ok := v.(string); ok {
					values = append(values, str)
				} else {
					return mcp.NewToolResultError(fmt.Sprintf("values[%d] in change %d must be a string", idx, i)), nil
				}
			}
		}

		mods = append(mods, ldapclient.Modification{
			Operation: ldapclient.ModifyOperation(strings.ToLower(opStr)),
			Attribute: attr,
			Values:    values,
		})
	}

	if err := t.client.ModifyEntry(ctx, dn, mods); err != nil {
		return toolErrorFromLDAP(err), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Entry %s modified successfully", dn)), nil
}

func (t *Toolset) handleDeleteEntry(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dn := mcp.ParseString(request, "dn", "")
	if strings.TrimSpace(dn) == "" {
		return mcp.NewToolResultError("dn parameter is required"), nil
	}

	if err := t.client.DeleteEntry(ctx, dn); err != nil {
		return toolErrorFromLDAP(err), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Entry %s deleted successfully", dn)), nil
}

func parseStringSlice(request mcp.CallToolRequest, key string) []string {
	raw := mcp.ParseArgument(request, key, nil)
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				items = append(items, str)
			}
		}
		return items
	case []string:
		return v
	default:
		return nil
	}
}

func decodeAttributes(raw map[string]any) (map[string][]string, error) {
	attributes := make(map[string][]string, len(raw))
	for key, value := range raw {
		list, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("attribute %s must be an array of strings", key)
		}
		values := make([]string, 0, len(list))
		for idx, item := range list {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("attribute %s value at index %d must be a string", key, idx)
			}
			values = append(values, str)
		}
		attributes[key] = values
	}
	return attributes, nil
}

func newJSONResult(payload map[string]any, message string) *mcp.CallToolResult {
	jsonBytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to encode response: %v", err))
	}

	return mcp.NewToolResultText(fmt.Sprintf("%s\n```json\n%s\n```", message, string(jsonBytes)))
}

func toolErrorFromLDAP(err error) *mcp.CallToolResult {
	switch {
	case errors.Is(err, ldapclient.ErrNotFound):
		return mcp.NewToolResultError("LDAP entry not found")
	default:
		return mcp.NewToolResultError(err.Error())
	}
}
