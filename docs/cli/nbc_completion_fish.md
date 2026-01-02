## nbc completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	nbc completion fish | source

To load completions for every new session, execute once:

	nbc completion fish > ~/.config/fish/completions/nbc.fish

You will need to start a new shell for this setup to take effect.


```
nbc completion fish [flags]
```

### Options

```
  -h, --help              help for fish
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc completion](nbc_completion.md)	 - Generate the autocompletion script for the specified shell

