## nbc completion bash

Generate the autocompletion script for bash

### Synopsis

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(nbc completion bash)

To load completions for every new session, execute once:

#### Linux:

	nbc completion bash > /etc/bash_completion.d/nbc

#### macOS:

	nbc completion bash > $(brew --prefix)/etc/bash_completion.d/nbc

You will need to start a new shell for this setup to take effect.


```
nbc completion bash
```

### Options

```
  -h, --help              help for bash
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

