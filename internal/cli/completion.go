package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gridhra/port-keeper-mcp/internal/app"
	"github.com/gridhra/port-keeper-mcp/internal/manifest"
	"github.com/gridhra/port-keeper-mcp/internal/render"
)

// commands is what `__complete command` offers; hidden commands are not listed.
var commands = []string{"init", "slot", "env", "url", "status", "gc", "doctor", "pin", "unpin", "reassign", "mcp", "context", "hook", "completion", "version"}

// cmdCompletion prints the static completion script for a shell. The scripts
// ask the hidden `port-keeper __complete <kind>` for dynamic candidates.
func cmdCompletion(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: port-keeper completion zsh|bash|fish")
	}
	var script string
	switch args[0] {
	case "zsh":
		script = zshCompletion
	case "bash":
		script = bashCompletion
	case "fish":
		script = fishCompletion
	default:
		return fmt.Errorf("completion: unsupported shell %q (zsh|bash|fish)", args[0])
	}
	_, err := io.WriteString(stdout, script)
	return err
}

// cmdComplete is the hidden `__complete <kind>` command behind the completion
// scripts. It prints one candidate per line and nothing at all on any error or
// outside a project: a completion must never print an error into the command
// line. Candidates are names only, never port numbers.
func cmdComplete(ctx context.Context, cwd string, args []string, stdout io.Writer) {
	if len(args) != 1 {
		return
	}
	var items []string
	switch args[0] {
	case "command":
		items = commands
	case "format":
		items = render.Formats
	case "slot-sub":
		items = []string{"new", "ls", "rm"}
	case "service":
		path, err := manifest.Find(cwd)
		if err != nil {
			return
		}
		m, err := manifest.Load(path)
		if err != nil {
			return
		}
		items = m.ServiceNames()
	case "slot":
		a, err := app.New(app.ActorCLI)
		if err != nil {
			return
		}
		defer a.Close()
		c, err := a.Resolve(ctx, cwd, "")
		if err != nil || c.Project == nil {
			return
		}
		slots, err := a.Ledger.ListSlots(ctx, c.Project.ID)
		if err != nil {
			return
		}
		for _, s := range slots {
			items = append(items, s.Name)
		}
	default:
		return
	}
	for _, it := range items {
		fmt.Fprintln(stdout, it)
	}
}

// The scripts avoid literal backquotes (they live in Go raw strings); every
// command substitution is written as $(...).

const zshCompletion = `#compdef port-keeper
# port-keeper zsh completion. Install: eval "$(port-keeper completion zsh)"
# or write it to a file on $fpath named _port-keeper.

_port_keeper_lines() { local -a r; r=(${(f)"$(port-keeper __complete "$1" 2>/dev/null)"}); compadd -a r; }

_port_keeper() {
  local -a words_no_slot
  local i=1
  # Drop the global --slot <name> so the subcommand is found at position 1.
  while (( i <= $#words )); do
    if [[ $words[i] == --slot ]]; then (( i += 2 )); continue; fi
    if [[ $words[i] == --slot=* ]]; then (( i += 1 )); continue; fi
    words_no_slot+=("$words[i]"); (( i += 1 ))
  done
  if [[ $words[CURRENT-1] == --slot || $words[CURRENT-1] == --infra-from ]]; then
    _port_keeper_lines slot; return
  fi
  if [[ $words[CURRENT] == --slot=* ]]; then
    compadd -P '--slot=' -- $(port-keeper __complete slot 2>/dev/null); return
  fi
  local cmd=${words_no_slot[2]}
  if (( ${#words_no_slot} <= 2 )) && [[ $words[CURRENT] != -* || -z $cmd ]]; then
    local -a cmds; cmds=(${(f)"$(port-keeper __complete command 2>/dev/null)"})
    compadd -a cmds; compadd -- --slot --help --version; return
  fi
  case $cmd in
    slot)
      local sub=${words_no_slot[3]}
      if (( ${#words_no_slot} <= 3 )) && [[ $words[CURRENT] == $sub ]]; then _port_keeper_lines slot-sub; return; fi
      case $sub in
        new) compadd -- --from-branch --infra-from --no-bind ;;
        ls) compadd -- --pins ;;
        rm) _port_keeper_lines slot; compadd -- --force --cascade ;;
      esac ;;
    env)
      if [[ $words[CURRENT-1] == --format ]]; then _port_keeper_lines format; return; fi
      compadd -- --format --stdout --if-present ;;
    url|reassign|pin|unpin) _port_keeper_lines service
      case $cmd in
        url) compadd -- --open ;;
        pin) compadd -- --reason --force ;;
        unpin) compadd -- --all ;;
      esac ;;
    status) compadd -- --json ;;
    context) compadd -- --json --if-present ;;
    doctor) compadd -- --fix ;;
    gc) compadd -- --yes ;;
    init) compadd -- --name --here ;;
    hook) compadd -- claude ;;
    completion) compadd -- zsh bash fish ;;
  esac
}

_port_keeper "$@"
`

const bashCompletion = `# port-keeper bash completion. Install: eval "$(port-keeper completion bash)"
_port_keeper() {
  local cur prev cmd sub i n
  COMPREPLY=()
  cur=${COMP_WORDS[COMP_CWORD]}
  prev=${COMP_WORDS[COMP_CWORD-1]}
  if [[ $prev == --slot || $prev == --infra-from ]]; then
    COMPREPLY=($(compgen -W "$(port-keeper __complete slot 2>/dev/null)" -- "$cur")); return
  fi
  # Find the subcommand, skipping the global --slot <name>.
  cmd=""; sub=""; n=0
  for (( i=1; i < COMP_CWORD; i++ )); do
    case ${COMP_WORDS[i]} in
      --slot) (( i++ )); continue ;;
      --slot=*) continue ;;
      -*) continue ;;
    esac
    if [[ -z $cmd ]]; then cmd=${COMP_WORDS[i]}; elif [[ -z $sub ]]; then sub=${COMP_WORDS[i]}; fi
    (( n++ ))
  done
  if [[ -z $cmd ]]; then
    COMPREPLY=($(compgen -W "$(port-keeper __complete command 2>/dev/null) --slot --help --version" -- "$cur")); return
  fi
  local words=""
  case $cmd in
    slot)
      if [[ -z $sub ]]; then words="new ls rm"
      else
        case $sub in
          new) words="--from-branch --infra-from --no-bind" ;;
          ls) words="--pins" ;;
          rm) words="$(port-keeper __complete slot 2>/dev/null) --force --cascade" ;;
        esac
      fi ;;
    env)
      if [[ $prev == --format ]]; then words=$(port-keeper __complete format 2>/dev/null)
      else words="--format --stdout --if-present"; fi ;;
    url) words="$(port-keeper __complete service 2>/dev/null) --open" ;;
    reassign) words=$(port-keeper __complete service 2>/dev/null) ;;
    pin) words="$(port-keeper __complete service 2>/dev/null) --reason --force" ;;
    unpin) words="$(port-keeper __complete service 2>/dev/null) --all" ;;
    status) words="--json" ;;
    context) words="--json --if-present" ;;
    doctor) words="--fix" ;;
    gc) words="--yes" ;;
    init) words="--name --here" ;;
    hook) words="claude" ;;
    completion) words="zsh bash fish" ;;
  esac
  COMPREPLY=($(compgen -W "$words" -- "$cur"))
}
complete -F _port_keeper port-keeper
`

const fishCompletion = `# port-keeper fish completion. Install: port-keeper completion fish > ~/.config/fish/completions/port-keeper.fish
complete -c port-keeper -f
complete -c port-keeper -n '__fish_use_subcommand' -a '(port-keeper __complete command 2>/dev/null)'
complete -c port-keeper -l slot -x -a '(port-keeper __complete slot 2>/dev/null)' -d 'act on this slot'
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and not __fish_seen_subcommand_from new ls rm' -a 'new ls rm'
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and __fish_seen_subcommand_from new' -l from-branch -d 'name the slot after the git branch'
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and __fish_seen_subcommand_from new' -l infra-from -x -a '(port-keeper __complete slot 2>/dev/null)'
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and __fish_seen_subcommand_from new' -l no-bind
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and __fish_seen_subcommand_from ls' -l pins
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and __fish_seen_subcommand_from rm' -a '(port-keeper __complete slot 2>/dev/null)'
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and __fish_seen_subcommand_from rm' -l force
complete -c port-keeper -n '__fish_seen_subcommand_from slot; and __fish_seen_subcommand_from rm' -l cascade
complete -c port-keeper -n '__fish_seen_subcommand_from env' -l format -x -a '(port-keeper __complete format 2>/dev/null)'
complete -c port-keeper -n '__fish_seen_subcommand_from env' -l stdout
complete -c port-keeper -n '__fish_seen_subcommand_from env' -l if-present
complete -c port-keeper -n '__fish_seen_subcommand_from url reassign pin unpin' -a '(port-keeper __complete service 2>/dev/null)'
complete -c port-keeper -n '__fish_seen_subcommand_from url' -l open
complete -c port-keeper -n '__fish_seen_subcommand_from pin' -l reason -x
complete -c port-keeper -n '__fish_seen_subcommand_from pin' -l force
complete -c port-keeper -n '__fish_seen_subcommand_from unpin' -l all
complete -c port-keeper -n '__fish_seen_subcommand_from status' -l json
complete -c port-keeper -n '__fish_seen_subcommand_from context' -l json
complete -c port-keeper -n '__fish_seen_subcommand_from context' -l if-present
complete -c port-keeper -n '__fish_seen_subcommand_from doctor' -l fix
complete -c port-keeper -n '__fish_seen_subcommand_from gc' -l yes
complete -c port-keeper -n '__fish_seen_subcommand_from init' -l name -x
complete -c port-keeper -n '__fish_seen_subcommand_from init' -l here
complete -c port-keeper -n '__fish_seen_subcommand_from hook' -a claude
complete -c port-keeper -n '__fish_seen_subcommand_from completion' -a 'zsh bash fish'
`
