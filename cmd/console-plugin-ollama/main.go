// Command console-plugin-ollama is a Console LLM plugin backed by a local Ollama
// server's chat API, served over gRPC (hashicorp/go-plugin). The host (console)
// launches it as a subprocess; it is not run directly.
//
// It requires no API key. The server address is taken from OLLAMA_HOST (else
// http://localhost:11434) and CONSOLE_MODEL optionally overrides the default
// model.
package main

import (
	"os"

	"github.com/moosequest/console/internal/llm/ollama"
	"github.com/moosequest/console/internal/plugin"
)

func main() {
	var opts []ollama.Option
	if model := os.Getenv("CONSOLE_MODEL"); model != "" {
		opts = append(opts, ollama.WithModel(model))
	}
	plugin.ServeLLM(ollama.New(opts...))
}
