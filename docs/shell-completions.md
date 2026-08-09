# Shell completions

SB Heartbeat generates completion scripts for Bash, Zsh, Fish, and PowerShell.
Generation writes the script to standard output, does not read project
configuration or environment-variable values, and never runs a heartbeat.

To enable completion in the current shell session:

### Bash

```bash
source <(sb-heartbeat completion bash)
```

### Zsh

```zsh
source <(sb-heartbeat completion zsh)
```

### Fish

```fish
sb-heartbeat completion fish | source
```

### PowerShell

```powershell
sb-heartbeat completion powershell | Out-String | Invoke-Expression
```

Run `sb-heartbeat completion <shell> --help` for shell-specific persistent
installation instructions. Install a generated script only from the exact SB
Heartbeat binary version you intend to use; regeneration after an upgrade keeps
commands and flags current.
