// Command console-plugin-anthropic is a Console LLM plugin backed by Anthropic's
// Claude Messages API, served over gRPC (hashicorp/go-plugin). The host
// (console) launches it as a subprocess; it is not run directly.
//
// It reads the API key from ANTHROPIC_API_KEY (inherited from the host) and
// exits if it is not set. CONSOLE_MODEL optionally overrides the default model.
package main

import (
	"fmt"
	"os"

	"github.com/moosequest/console/internal/llm/anthropic"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-anthropic: ANTHROPIC_API_KEY is required")
		os.Exit(1)
	}
	var opts []anthropic.Option
	if model := os.Getenv("CONSOLE_MODEL"); model != "" {
		opts = append(opts, anthropic.WithModel(model))
	}
	plugin.ServeLLM(anthropic.New(key, opts...))
}
