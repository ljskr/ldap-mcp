# LDAP MCP Server

LDAP MCP Server exposes an LDAP directory through the Model Context Protocol (MCP), enabling MCP clients to run common directory searches and CRUD operations using the standard tool/resource workflow.

## Features
- Search, retrieve, add, modify, and delete LDAP entries through MCP tools.
- Optional read-only mode that limits MCP clients to safe operations.
- Support for StartTLS upgrades, LDAPS endpoints, and configurable TLS verification.
- Built-in MCP resources for the directory root DSE and arbitrary entries by DN.
- Graceful shutdown handling and automatic LDAP reconnection logic.

## Requirements
- Go 1.24 or newer
- Access to an LDAP server (OpenLDAP, Active Directory, etc.)
- MCP client capable of speaking the SSE transport (e.g., Claude Desktop)

## Building and Testing
```sh
cd /opt/code/github/ldap-mcp
go test ./...
```
The repository currently ships without unit tests, so a successful run confirms the project compiles and all dependencies resolve.

## Running the Server
```sh
cd /opt/code/github/ldap-mcp
go run ./cmd/server \
  -url ldap://localhost:389 \
  -bind-dn "cn=admin,dc=example,dc=com" \
  -bind-password secret
```
Key flags:
- `-addr`: MCP listen address (default `:8080`, overridable via `MCP_PORT`).
- `-url`: LDAP server URL such as `ldap://host:389` or `ldaps://host:636`.
- `-bind-dn` / `-bind-password`: Credentials for binding to the directory. You can also supply the password via the `LDAP_BIND_PASSWORD` environment variable.
- `-starttls`: Upgrade a plain LDAP connection to TLS. Only valid when using `ldap://` URLs.
- `-insecure`: Skip TLS certificate verification (useful for testing with self-signed certs).
- `-read-write`: Enable add/modify/delete tools. If omitted the server operates in read-only mode.
- `-timeout`: Per-request timeout when talking to the LDAP server (default 30s).

Use `-help` to print the full list of flags and environment variables.

## MCP Surface
**Tools**
- `search_entries`: Execute LDAP searches with paging, scope selection, alias dereferencing, and size limits.
- `get_entry`: Fetch a single entry by distinguished name.
- `add_entry`: Create new entries (requires `-read-write`).
- `modify_entry`: Apply attribute modifications (requires `-read-write`).
- `delete_entry`: Delete entries (requires `-read-write`).

**Resources**
- `ldap://root-dse`: Returns the directory root DSE as JSON.
- `ldap://entry/{dn}`: Fetches a specific entry when provided with a DN.

## Development Notes
- The LDAP client wrapper (`internal/ldapclient`) manages connection reuse, StartTLS negotiation, and automatic reconnection on transport errors.
- MCP tool handlers (`internal/tools`) validate inputs before invoking LDAP operations; for example the `page_size` argument is clamped to the `uint32` range used by paged results controls.
- Format Go sources with `gofmt` and keep module dependencies tidy via `go mod tidy` when dependencies change.

## Docker

create a `.env` file with the following variables:
```
LDAP_URL=ldap://localhost:389
LDAP_BIND_DN=cn=admin,dc=example,dc=com
LDAP_BIND_PASSWORD=secret
```

build the image:

```bash
docker compose build
```

start the container:

```bash
docker compose up -d
```
The server will be available at `http://localhost:8080`.

## MCP Client

### Cursor

```json
{
  "mcpServers": {
    "ldap-mcp": {
      "type": "SSE",
      "url": "http://localhost:8080/sse"
    }
  }
}
```
