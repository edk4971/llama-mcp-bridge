package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authRoundTripper struct {
	base http.RoundTripper
	token string
}

func (a *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if a.token != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer " + a.token)
	}
	if a.base == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return a.base.RoundTrip(req)
}


func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: mcp-bridge <prefix> <url> [log]\n")
		os.Exit(1)
	}
	prefix := os.Args[1]
	urlStr := os.Args[2]

	logEnabled := false
	var logFile *os.File
	var log func(string)
	if len(os.Args) >= 4 && os.Args[3] == "log" {
		logPath := os.Getenv("LOGFILE")
		if logPath == "" {
			logPath = "/config/mcp-bridge.log"
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			logFile = f
			logEnabled = true
		}
	}
	if logEnabled {
		defer logFile.Close()
		log = func(s string) {
			logFile.WriteString(s + "\n")
		}
	} else {
		log = func(s string) {}
	}

	transportType := strings.ToLower(os.Getenv("MCP_TRANSPORT"))
	if transportType == "" {
		transportType = "sse"
	}
	token := os.Getenv("MCP_BEARER_TOKEN")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-bridge", Version: "1.0"}, nil)

	// Build HTTP client with Bearer token if provided
	var httpClient *http.Client
	if token != "" {
		httpClient = &http.Client{Transport: &authRoundTripper{base: http.DefaultTransport, token: token}}
	}

	var transport mcp.Transport
	switch transportType {
	case "sse":
		transport = &mcp.SSEClientTransport{Endpoint: urlStr, HTTPClient: httpClient}
	case "streamable", "http":
		transport = &mcp.StreamableClientTransport{Endpoint: urlStr, DisableStandaloneSSE: true, HTTPClient: httpClient}
	default:
		logStderr(fmt.Sprintf("[mcp-bridge] unknown transport %s", transportType))
		os.Exit(1)
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		logStderr(fmt.Sprintf("[mcp-bridge] connect failed: %v", err))
		os.Exit(1)
	}
	defer session.Close()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		log("IN:" + string(line))
		if len(line) == 0 {
			continue
		}
		var req map[string]interface{}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		id := req["id"]

		switch method {
		case "initialize":
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{"listChanged": false},
					},
					"serverInfo": map[string]interface{}{
						"name":    prefix,
						"version": "1.0",
					},
				},
			}
			if out, _ := json.Marshal(resp); true {
				s := string(out)
				log("OUT:" + s)
				fmt.Println(s)
			}
		case "tools/list":
			tools, err := session.ListTools(ctx, nil)
			if err != nil {
				continue
			}
			toolsOut := []map[string]interface{}{}
			for _, t := range tools.Tools {
				name := t.Name
				if !strings.HasPrefix(name, prefix+"_") {
					name = prefix + "_" + t.Name
				}
				toolsOut = append(toolsOut, map[string]interface{}{
					"name":        name,
					"description": t.Description,
				})
			}
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"tools": toolsOut,
				},
			}
			if out, _ := json.Marshal(resp); true {
				s := string(out)
				log("OUT:" + s)
				fmt.Println(s)
			}
		case "tools/call":
			params, _ := req["params"].(map[string]interface{})
			name, _ := params["name"].(string)
			if strings.HasPrefix(name, prefix+"_") {
				name = strings.TrimPrefix(name, prefix+"_")
			}
			callParams := &mcp.CallToolParams{
				Name:      name,
				Arguments: params["arguments"],
			}
			res, err := session.CallTool(ctx, callParams)
			if err != nil {
				continue
			}
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"content": res.Content,
					"isError": res.IsError,
				},
			}
			if out, _ := json.Marshal(resp); true {
				s := string(out)
				log("OUT:" + s)
				fmt.Println(s)
			}
		}
	}
}
