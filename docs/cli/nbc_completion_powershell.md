## nbc completion powershell

Generate the autocompletion script for powershell

### Synopsis

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	nbc completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.


```
nbc completion powershell [flags]
```

### Options

```
  -h, --help              help for powershell
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

