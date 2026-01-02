## nbc completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(nbc completion zsh)

To load completions for every new session, execute once:

#### Linux:

	nbc completion zsh > "${fpath[1]}/_nbc"

#### macOS:

	nbc completion zsh > $(brew --prefix)/share/zsh/site-functions/_nbc

You will need to start a new shell for this setup to take effect.


```
nbc completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
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

