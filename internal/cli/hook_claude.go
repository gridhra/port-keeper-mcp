// Claude Code adapter. The core contract for any agent client is
// `port-keeper context --json` (where am I, what should I do) and
// `port-keeper env --format export --if-present` (the environment). This file
// only translates between that contract and Claude Code's hook protocol
// (JSON on stdin, CLAUDE_ENV_FILE, hookSpecificOutput on stdout). Other
// clients get their own adapter file; the core stays client-agnostic.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gridhra/port-keeper-mcp/internal/app"
	"github.com/gridhra/port-keeper-mcp/internal/manifest"
)

// hookInput is the subset of the JSON Claude Code passes to hooks on stdin.
type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	Cwd           string `json:"cwd"`
	NewCwd        string `json:"new_cwd"`
}

// cmdHook implements the Claude Code hook. It reads the hook JSON from stdin
// (when stdin is not a terminal), resolves the project from the directory the
// event names, appends this slot's environment to $CLAUDE_ENV_FILE, and prints
// the JSON the event expects: additionalContext for SessionStart, a brief
// systemMessage for CwdChanged. Outside a project it prints nothing and exits 0.
// `hook claude` serves both events; `hook claude-session-start` is an alias.
func cmdHook(ctx context.Context, a *app.App, cwd, slot string, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 1 || (args[0] != "claude" && args[0] != "claude-session-start") {
		return errors.New("usage: port-keeper hook claude   (register it for SessionStart and CwdChanged)")
	}
	in := readHookInput(stdin)
	event := in.HookEventName
	if event == "" {
		event = "SessionStart"
	}
	dir := cwd
	if in.NewCwd != "" {
		dir = in.NewCwd
	} else if in.Cwd != "" {
		dir = in.Cwd
	}
	info, c, err := contextInfo(ctx, a, dir, slot)
	if err != nil {
		if errors.Is(err, manifest.ErrNotFound) {
			return nil
		}
		return err
	}
	if !info.Ready {
		// Do not create slots from a hook; just relay the core's guidance.
		return printHook(stdout, event, "port-keeper: "+info.Guidance)
	}
	text, _, err := a.EnvText(ctx, c, "export")
	if err != nil {
		return err
	}
	if envFile := os.Getenv("CLAUDE_ENV_FILE"); envFile != "" {
		f, err := os.OpenFile(envFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, text); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	msg := "port-keeper: " + info.Guidance
	for _, w := range info.Warnings {
		msg += " Note: " + w + "."
	}
	return printHook(stdout, event, msg)
}

func readHookInput(stdin io.Reader) hookInput {
	var in hookInput
	if f, ok := stdin.(*os.File); ok {
		if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
			return in // interactive terminal: nothing to read
		}
	}
	b, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return in
	}
	_ = json.Unmarshal(b, &in)
	return in
}

// printHook writes the JSON reply for the event. SessionStart accepts
// additionalContext; CwdChanged only shows systemMessage, so the context is
// condensed to its first sentence there.
func printHook(w io.Writer, event, text string) error {
	var doc map[string]any
	switch event {
	case "SessionStart":
		doc = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": text}}
	default:
		short := text
		if i := strings.Index(short, ". "); i > 0 {
			short = short[:i+1]
		}
		doc = map[string]any{"systemMessage": short}
	}
	enc, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(enc))
	return err
}
