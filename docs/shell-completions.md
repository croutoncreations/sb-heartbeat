# Shell completions

SB Heartbeat generates completion scripts for Bash, Zsh, Fish, and PowerShell.
Generation writes the script to standard output, does not read project
configuration or environment-variable values, and never runs a heartbeat.

To enable completion in the current shell session:

## Bash

Install and load [`bash-completion`](https://github.com/scop/bash-completion)
for the Bash version you run, then source the generated script:

```bash
source <(sb-heartbeat completion bash)
```

## Zsh

Initialize Zsh's completion system before sourcing the generated script:

```zsh
autoload -U compinit && compinit
source <(sb-heartbeat completion zsh)
```

## Fish

```fish
sb-heartbeat completion fish | source
```

## PowerShell

```powershell
sb-heartbeat completion powershell | Out-String | Invoke-Expression
```

Run `sb-heartbeat completion <shell> --help` for shell-specific persistent
installation instructions. Install a generated script only from the exact SB
Heartbeat binary version you intend to use; regeneration after an upgrade keeps
commands and flags current.
