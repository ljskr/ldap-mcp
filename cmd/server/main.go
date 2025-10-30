package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/trxo/ldap-mcp/internal/ldapclient"
	"github.com/trxo/ldap-mcp/internal/resources"
	"github.com/trxo/ldap-mcp/internal/tools"
)

const (
	defaultLDAPURL = "ldap://localhost:389"
	defaultTimeout = 30 * time.Second
)

type config struct {
	addr           string
	ldapURL        string
	bindDN         string
	bindPassword   string
	useStartTLS    bool
	insecureTLS    bool
	readWrite      bool
	help           bool
	requestTimeout time.Duration
}

func main() {
	cfg := parseFlags()
	if cfg.help {
		showHelp()
		return
	}

	ctx := setupContext()

	client := initializeLDAPClient(cfg)
	defer closeLDAPClient(client)

	mcpServer := createMCPServer()
	registerToolsAndResources(mcpServer, client, cfg.readWrite)

	runServer(ctx, mcpServer, cfg.addr, cfg)
}

func parseFlags() config {
	ldapURL := flag.String("url", defaultLDAPURL, "LDAP server URL (e.g. ldap://localhost:389 or ldaps://localhost:636)")
	addr := flag.String("addr", getDefaultAddress(), "Address to listen on for MCP (use :port)")
	bindDN := flag.String("bind-dn", "", "Bind DN used to authenticate against the LDAP server")
	bindPassword := flag.String("bind-password", "", "Bind password used to authenticate against the LDAP server")
	startTLS := flag.Bool("starttls", false, "Use StartTLS after connecting (only valid with ldap:// URIs)")
	insecureTLS := flag.Bool("insecure", false, "Skip TLS certificate verification (use with caution)")
	readWrite := flag.Bool("read-write", false, "Allow write operations (add/modify/delete). When false the server runs in read-only mode")
	timeout := flag.Duration("timeout", defaultTimeout, "Per-request timeout when talking to LDAP")
	help := flag.Bool("help", false, "Show help message")

	flag.Parse()

	return config{
		addr:           *addr,
		ldapURL:        *ldapURL,
		bindDN:         *bindDN,
		bindPassword:   *bindPassword,
		useStartTLS:    *startTLS,
		insecureTLS:    *insecureTLS,
		readWrite:      *readWrite,
		help:           *help,
		requestTimeout: *timeout,
	}
}

func showHelp() {
	fmt.Fprintf(os.Stdout, "LDAP MCP Server - a Model Context Protocol server for LDAP directories\n\n")
	fmt.Fprintf(os.Stdout, "Usage: %s [options]\n\n", os.Args[0])
	fmt.Fprintf(os.Stdout, "Options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stdout, "\nEnvironment Variables:\n")
	fmt.Fprintf(os.Stdout, "  MCP_PORT    Port to listen on (overrides -addr flag port)\n")
	fmt.Fprintf(os.Stdout, "  LDAP_BIND_PASSWORD    Bind password (alternative to -bind-password)\n")
	fmt.Fprintf(os.Stdout, "\nExamples:\n")
	fmt.Fprintf(os.Stdout, "  %s -url ldap://127.0.0.1:389 -bind-dn \"cn=admin,dc=example,dc=com\" -bind-password secret\n", os.Args[0])
	fmt.Fprintf(os.Stdout, "  MCP_PORT=9000 %s -url ldaps://ldap.example.com:636 -bind-dn \"cn=service,dc=example,dc=com\"\n", os.Args[0])
}

func setupContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Received shutdown signal")
		cancel()
	}()

	return ctx
}

func initializeLDAPClient(cfg config) *ldapclient.Client {
	password := cfg.bindPassword
	if password == "" {
		password = os.Getenv("LDAP_BIND_PASSWORD")
	}

	client, err := ldapclient.New(ldapclient.Config{
		URL:            cfg.ldapURL,
		BindDN:         cfg.bindDN,
		BindPassword:   password,
		UseStartTLS:    cfg.useStartTLS,
		InsecureTLS:    cfg.insecureTLS,
		DefaultTimeout: cfg.requestTimeout,
	})
	if err != nil {
		log.Fatalf("Failed to connect to LDAP server: %v", err)
	}

	return client
}

func closeLDAPClient(client *ldapclient.Client) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		log.Printf("Error while closing LDAP client: %v", err)
	}
}

func createMCPServer() *server.MCPServer {
	return server.NewMCPServer(
		"ldap-mcp",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, false),
		server.WithLogging(),
		server.WithRecovery(),
	)
}

func registerToolsAndResources(mcpServer *server.MCPServer, client *ldapclient.Client, readWrite bool) {
	toolset := tools.New(client)
	restResources := resources.New(client)

	for _, tool := range toolset.GetTools(readWrite) {
		mcpServer.AddTool(tool, toolset.HandleTool)
	}

	for _, resource := range restResources.GetResources() {
		mcpServer.AddResource(resource, restResources.HandleResource)
	}

	for _, template := range restResources.GetResourceTemplates() {
		mcpServer.AddResourceTemplate(template, restResources.HandleResource)
	}
}

func runServer(ctx context.Context, mcpServer *server.MCPServer, addr string, cfg config) {
	sseServer := server.NewSSEServer(mcpServer)

	errChan := make(chan error, 1)
	go func() {
		logServerStart(addr, cfg)
		errChan <- sseServer.Start(addr)
	}()

	select {
	case err := <-errChan:
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case <-ctx.Done():
		log.Println("Shutting down server...")
	}

	log.Println("Server shutdown complete")
}

func logServerStart(addr string, cfg config) {
	mode := "read-only"
	if cfg.readWrite {
		mode = "read-write"
	}

	proto := "StartTLS"
	if cfg.useStartTLS {
		proto = "StartTLS"
	} else if cfg.ldapURL != "" {
		proto = cfg.ldapURL
	}

	log.Printf("Starting LDAP MCP Server on %s (%s mode)", addr, mode)
	log.Printf("LDAP URL: %s", cfg.ldapURL)
	if cfg.bindDN != "" {
		log.Printf("Bind DN: %s", cfg.bindDN)
	} else {
		log.Printf("Bind DN: (anonymous)")
	}
	log.Printf("TLS mode: %s (insecure=%t)", proto, cfg.insecureTLS)
	log.Printf("Available tools: %s", availableTools(cfg.readWrite))
	log.Printf("Available resources: ldap://root-dse, ldap://entry/{dn}")
}

func availableTools(readWrite bool) string {
	if readWrite {
		return "search_entries, get_entry, add_entry, modify_entry, delete_entry"
	}
	return "search_entries, get_entry"
}

func getDefaultAddress() string {
	port := "8080"
	if envPort := os.Getenv("MCP_PORT"); envPort != "" {
		if portNum, err := strconv.Atoi(envPort); err == nil {
			if portNum >= 0 && portNum <= 65535 {
				port = envPort
			} else {
				log.Printf("Invalid MCP_PORT value: %s (must be between 0 and 65535), using default port 8080", envPort)
			}
		} else {
			log.Printf("Invalid MCP_PORT value: %s (must be numeric), using default port 8080", envPort)
		}
	}
	return ":" + port
}
