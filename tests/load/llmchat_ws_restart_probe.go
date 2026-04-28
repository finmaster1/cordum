//go:build ignore

// Host-driven WebSocket restart probe for cordum-llm-chat. Run from repo root:
//
//	CORDUM_API_KEY=<key> go run tests/load/llmchat_ws_restart_probe.go
//
// It opens a direct trusted-forwarder WebSocket to llm-chat, sends one prompt,
// restarts the configured Compose inference service, and prints observed frames.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	key := os.Getenv("CORDUM_API_KEY")
	if key == "" {
		log.Fatal("CORDUM_API_KEY required")
	}
	url := getenv("LLMCHAT_WS_URL", "ws://127.0.0.1:8090/api/v1/chat/ws")
	service := getenv("INFERENCE_SERVICE", "ollama")

	h := http.Header{}
	h.Set("X-API-Key", key)
	h.Set("X-Cordum-Tenant", "default")
	h.Set("X-Cordum-Principal", "prod-readiness")
	h.Set("X-Cordum-Role", "admin")
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	c, resp, err := dialer.Dial(url, h)
	if err != nil {
		if resp != nil {
			log.Fatalf("dial: %v status=%s", err, resp.Status)
		}
		log.Fatalf("dial: %v", err)
	}
	defer c.Close()
	fmt.Println("ws_open=ok")
	msg := `{"message":"For a production-readiness restart probe, reply with exactly one short sentence."}`
	if err := c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		log.Fatalf("write: %v", err)
	}
	fmt.Println("sent_message=ok")

	go func() {
		time.Sleep(2 * time.Second)
		cmd := exec.Command("docker", "compose", "restart", service)
		cmd.Dir = getenv("CORDUM_REPO_ROOT", "D:/Cordum/cordum")
		out, err := cmd.CombinedOutput()
		fmt.Printf("docker_restart_%s_err=%v\n", service, err)
		fmt.Printf("docker_restart_%s_output=%s\n", service, strings.TrimSpace(string(out)))
	}()

	deadline := time.Now().Add(75 * time.Second)
	for {
		_ = c.SetReadDeadline(time.Now().Add(75 * time.Second))
		mt, payload, err := c.ReadMessage()
		if err != nil {
			fmt.Printf("ws_read_error=%T %v\n", err, err)
			break
		}
		s := strings.ReplaceAll(string(payload), "\n", " ")
		if len(s) > 400 {
			s = s[:400] + "..."
		}
		fmt.Printf("frame_at=%s type=%d payload=%s\n", time.Now().Format(time.RFC3339), mt, s)
		if strings.Contains(s, `"type":"final"`) || strings.Contains(s, `"type":"error"`) || time.Now().After(deadline) {
			break
		}
	}
}

func getenv(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}
