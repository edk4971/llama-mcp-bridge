package main

import (
        "bufio"
        "bytes"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "os"
        "strings"
        "sync"
        "sync/atomic"
        "time"
)

var toolsListIDs sync.Map

func main() {
        if len(os.Args) < 3 {
                fmt.Fprintf(os.Stderr, "Usage: mcp-bridge <prefix> <url>\n")
                os.Exit(1)
        }
        prefix := os.Args[1]
        targetURLStr := os.Args[2]

        token := os.Getenv("MCP_BEARER_TOKEN")
        transport := strings.ToLower(os.Getenv("MCP_TRANSPORT"))
        if transport == "" {
                transport = "sse"
        }

        targetURL, err := url.Parse(targetURLStr)
        if err != nil {
                fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] Invalid URL: %v\n", prefix, err)
                os.Exit(1)
        }

        switch transport {
        case "sse":
                runSSETransport(prefix, targetURL, token)
        case "streamable", "http":
                runStreamableTransport(prefix, targetURL, token)
        default:
                fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] Unknown transport type: %s\n", prefix, transport)
                os.Exit(1)
        }
}

func runStreamableTransport(prefix string, targetURL *url.URL, token string) {
        scanner := bufio.NewScanner(os.Stdin)
        buf := make([]byte, 0, 64*1024)
        scanner.Buffer(buf, 10*1024*1024)

        client := &http.Client{Timeout: 30 * time.Second} // Add reasonable timeout to prevent hang [1.1]

        for scanner.Scan() {
                raw := bytes.Clone(scanner.Bytes()) // Clone to prevent buffer reuse mutation [1.1]

                var reqMap map[string]interface{}
                if err := json.Unmarshal(raw, &reqMap); err == nil {
                        method, _ := reqMap["method"].(string)
                        id := reqMap["id"]

                        if method == "tools/list" && id != nil {
                                toolsListIDs.Store(fmt.Sprintf("%v", id), struct{}{}) // Store empty struct, not bool [1.1]
                        } else if method == "tools/call" {
                                if params, ok := reqMap["params"].(map[string]interface{}); ok {
                                        if name, ok := params["name"].(string); ok {
                                                if strings.HasPrefix(name, prefix+"_") {
                                                        params["name"] = strings.TrimPrefix(name, prefix+"_")
                                                        raw, _ = json.Marshal(reqMap)
                                                }
                                        }
                                }
                        }
                }

                req, _ := http.NewRequest("POST", targetURL.String(), bytes.NewReader(raw))
                req.Header.Set("Content-Type", "application/json")
                req.Header.Set("Accept", "application/json, text/event-stream")
                req.Header.Set("MCP-Protocol-Version", "2026-07-28") // Required by July 2026 spec
                if token != "" {
                        req.Header.Set("Authorization", "Bearer "+token)
                }

                resp, err := client.Do(req)
                if err != nil {
                        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] POST failed: %v\n", prefix, err)
                        continue
                }

                if resp.StatusCode >= 400 {
                        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] HTTP Error %d\n", prefix, resp.StatusCode)
                }

                contentType := resp.Header.Get("Content-Type")
                if strings.HasPrefix(contentType, "text/event-stream") {
                        respScanner := bufio.NewScanner(resp.Body)
                        respScanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
                        var eventData strings.Builder
                        for respScanner.Scan() {
                                line := respScanner.Text()
                                if line == "" {
                                        if eventData.Len() > 0 {
                                                handleIncomingMessage([]byte(eventData.String()), prefix)
                                                eventData.Reset()
                                        }
                                        continue
                                }
                                if strings.HasPrefix(line, "data: ") {
                                        eventData.WriteString(strings.TrimPrefix(line, "data: "))
                                }
                        }
                } else {
                        body, _ := io.ReadAll(resp.Body)
                        handleIncomingMessage(body, prefix)
                }

                // Ensure body is fully drained for connection reuse [1.1]
                io.Copy(io.Discard, resp.Body)
                resp.Body.Close()
        }

        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] Stdin closed. Shutting down.\n", prefix)
        os.Exit(0)
}

func runSSETransport(prefix string, sseURL *url.URL, token string) {
        postURLChan := make(chan string, 1)
        var currentPostURL atomic.Value // Fix data race [1.1]

        go func() {
                client := &http.Client{Timeout: 0}
                for {
                        req, _ := http.NewRequest("GET", sseURL.String(), nil)
                        req.Header.Set("Accept", "text/event-stream")
                        if token != "" {
                                req.Header.Set("Authorization", "Bearer "+token)
                        }

                        resp, err := client.Do(req)
                        if err != nil || resp.StatusCode != 200 {
                                var code int
                                if resp != nil {
                                        code = resp.StatusCode
                                }
                                fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] SSE Connection failed (HTTP %d), retrying in 5s...\n", prefix, code)
                                if resp != nil {
                                        io.Copy(io.Discard, resp.Body)
                                        resp.Body.Close()
                                }
                                time.Sleep(5 * time.Second)
                                continue
                        }

                        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] Connected to SSE stream.\n", prefix)
                        scanner := bufio.NewScanner(resp.Body)

                        buf := make([]byte, 0, 64*1024)
                        scanner.Buffer(buf, 10*1024*1024)

                        var currentEvent string
                        var eventData strings.Builder

                        for scanner.Scan() {
                                line := scanner.Text()
                                if line == "" {
                                        if currentEvent == "endpoint" {
                                                uri := eventData.String()
                                                parsed, err := sseURL.Parse(uri)
                                                if err == nil {
                                                        select {
                                                        case postURLChan <- parsed.String():
                                                        default:
                                                                <-postURLChan
                                                                postURLChan <- parsed.String()
                                                        }
                                                }
                                        } else if currentEvent == "message" {
                                                handleIncomingMessage([]byte(eventData.String()), prefix)
                                        }
                                        currentEvent = ""
                                        eventData.Reset()
                                        continue
                                }

                                if strings.HasPrefix(line, "event: ") {
                                        currentEvent = strings.TrimPrefix(line, "event: ")
                                } else if strings.HasPrefix(line, "data: ") {
                                        if eventData.Len() > 0 {
                                                eventData.WriteString("\n")
                                        }
                                        eventData.WriteString(strings.TrimPrefix(line, "data: "))
                                }
                        }
                        resp.Body.Close()
                        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] SSE disconnected, reconnecting...\n", prefix)
                        time.Sleep(2 * time.Second)
                }
        }()

        select {
        case initialURL := <-postURLChan:
                currentPostURL.Store(initialURL) // Store atomically [1.1]
        case <-time.After(30 * time.Second):
                fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] Timeout waiting for endpoint\n", prefix)
                os.Exit(1)
        }

        go func() {
                for newURL := range postURLChan {
                        currentPostURL.Store(newURL) // Update atomically [1.1]
                }
        }()

        scanner := bufio.NewScanner(os.Stdin)
        buf := make([]byte, 0, 64*1024)
        scanner.Buffer(buf, 10*1024*1024)

        postClient := &http.Client{Timeout: 30 * time.Second}

        for scanner.Scan() {
                raw := bytes.Clone(scanner.Bytes()) // Clone to prevent buffer reuse mutation [1.1]

                var reqMap map[string]interface{}
                if err := json.Unmarshal(raw, &reqMap); err == nil {
                        method, _ := reqMap["method"].(string)
                        id := reqMap["id"]

                        if method == "tools/list" && id != nil {
                                toolsListIDs.Store(fmt.Sprintf("%v", id), struct{}{}) // Store empty struct [1.1]
                        } else if method == "tools/call" {
                                if params, ok := reqMap["params"].(map[string]interface{}); ok {
                                        if name, ok := params["name"].(string); ok {
                                                if strings.HasPrefix(name, prefix+"_") {
                                                        params["name"] = strings.TrimPrefix(name, prefix+"_")
                                                        raw, _ = json.Marshal(reqMap)
                                                }
                                        }
                                }
                        }
                }

                target := currentPostURL.Load().(string) // Safe atomic read [1.1]
                req, _ := http.NewRequest("POST", target, bytes.NewReader(raw))
                req.Header.Set("Content-Type", "application/json")
                if token != "" {
                        req.Header.Set("Authorization", "Bearer "+token)
                }

                resp, err := postClient.Do(req)
                if err != nil {
                        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] POST failed: %v\n", prefix, err)
                        continue
                }

                if resp.StatusCode >= 400 {
                        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] POST returned HTTP %d\n", prefix, resp.StatusCode)
                }

                // Ensure body is drained for connection reuse [1.1]
                io.Copy(io.Discard, resp.Body)
                resp.Body.Close()
        }

        fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] Stdin closed. Shutting down.\n", prefix)
        os.Exit(0)
}

func handleIncomingMessage(data []byte, prefix string) {
        var respMap map[string]interface{}
        if err := json.Unmarshal(data, &respMap); err == nil {
                if id := respMap["id"]; id != nil {
                        idStr := fmt.Sprintf("%v", id)

                        if _, exists := toolsListIDs.Load(idStr); exists {
                                toolsListIDs.Delete(idStr)
                                if result, ok := respMap["result"].(map[string]interface{}); ok {
                                        if tools, ok := result["tools"].([]interface{}); ok {
                                                for _, t := range tools {
                                                        if tool, ok := t.(map[string]interface{}); ok {
                                                                if name, ok := tool["name"].(string); ok {
                                                                        tool["name"] = prefix + "_" + name
                                                                }
                                                        }
                                                }
                                        }
                                }
                                data, _ = json.Marshal(respMap)
                        }
                }
                // Write to stdout ONLY if it successfully parsed [1.1]
                fmt.Printf("%s\n", string(data))
        } else {
                // Log failures to stderr to avoid corrupting llama.cpp [1.1]
                fmt.Fprintf(os.Stderr, "[mcp-bridge] [%s] Failed to parse message: %v\n", prefix, err)
        }
}
