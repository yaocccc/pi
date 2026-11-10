package main

import (
	"fmt"
	"strings"
)

// Only pass startup configuration options through. Mode/session/prompt options
// would bypass the frontend's RPC ownership and --check's no-session guarantee.
func piArgs(args []string) ([]string, error) {
	values := map[string]bool{"--provider": true, "--model": true, "--api-key": true, "--system-prompt": true, "--append-system-prompt": true, "--name": true, "-n": true, "--models": true, "--tools": true, "-t": true, "--exclude-tools": true, "-xt": true, "--thinking": true, "--extension": true, "-e": true, "--skill": true, "--prompt-template": true, "--theme": true, "--use-theme": true}
	switches := map[string]bool{"--no-tools": true, "-nt": true, "--no-builtin-tools": true, "-nbt": true, "--no-extensions": true, "-ne": true, "--no-mcp": true, "--no-skills": true, "-ns": true, "--no-prompt-templates": true, "-np": true, "--no-themes": true, "--no-context-files": true, "-nc": true, "--verbose": true, "--approve": true, "-a": true, "--no-approve": true, "-na": true, "--offline": true}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		name, value, eq := strings.Cut(args[i], "=")
		if switches[name] && !eq {
			out = append(out, name)
			continue
		}
		if !values[name] {
			return nil, fmt.Errorf("unsupported pi startup option %q: mode/session flags and positional prompts must not be passed through", name)
		}
		if !eq {
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("%s requires a value", name)
			}
			value = args[i]
		}
		if value == "" {
			return nil, fmt.Errorf("%s requires a nonempty value", name)
		}
		out = append(out, name, value)
	}
	return out, nil
}
