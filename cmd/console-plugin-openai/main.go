// Command console-plugin-openai is a Console LLM plugin backed by OpenAI's Chat
// Completions API, served over gRPC (hashicorp/go-plugin). The host (console)
// launches it as a subprocess; it is not run directly.
//
// It reads the API key from OPENAI_API_KEY (inherited from the host) and exits
// if it is not set. CONSOLE_MODEL optionally overrides the default model.
package main

import (
	"fmt"
	"os"

	"github.com/moosequest/console/internal/llm/openai"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "console-plugin-openai: OPENAI_API_KEY is required")
		os.Exit(1)
	}
	var opts []openai.Option
	if model := os.Getenv("CONSOLE_MODEL"); model != "" {
		opts = append(opts, openai.WithModel(model))
	}
	plugin.ServeLLM(openai.New(key, opts...))
}
