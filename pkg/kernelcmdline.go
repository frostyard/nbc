package pkg

// kernelCmdlineParams holds everything needed to build the kernel command line.
// Both the install and update flows populate this and call assembleKernelCmdline,
// so the two produce identical output and can never drift (see the "Install and
// Update Parity" note in CLAUDE.md).
type kernelCmdlineParams struct {
	Encrypted      bool
	TPM2           bool
	FilesystemType string // "ext4"/"btrfs"; defaults to ext4 when empty

	// Encrypted systems (device-mapper root + var):
	RootMapperName string // "root1" or "root2"
	RootLUKSUUID   string
	VarLUKSUUID    string

	// Non-encrypted systems (partition UUIDs):
	RootUUID string
	VarUUID  string

	// Always required:
	BootUUID  string
	ExtraArgs []string // user-supplied kernel arguments, appended last
}

// updateRootSlot picks which root slot a cmdline is being built for during an
// A/B update. The target (new) root is the currently-inactive slot; the
// non-target case is the active/previous slot. root1Active reports whether root1
// is the currently-active slot. It returns the mapper name ("root1"/"root2") and
// whether the second slot was chosen.
func updateRootSlot(isTarget, root1Active bool) (mapperName string, useRoot2 bool) {
	useRoot2 = (isTarget && root1Active) || (!isTarget && !root1Active)
	if useRoot2 {
		return "root2", true
	}
	return "root1", false
}

// assembleKernelCmdline builds the ordered kernel command line from params. It is
// pure (no I/O) so both flows share one authoritative definition of the cmdline
// format.
func assembleKernelCmdline(p kernelCmdlineParams) []string {
	fsType := p.FilesystemType
	if fsType == "" {
		fsType = "ext4"
	}

	var cmdline []string
	if p.Encrypted {
		// Root via device mapper.
		cmdline = append(cmdline, "root=/dev/mapper/"+p.RootMapperName, "ro")

		// LUKS UUIDs for the initramfs to discover and unlock.
		cmdline = append(cmdline,
			"rd.luks.uuid="+p.RootLUKSUUID,
			"rd.luks.name="+p.RootLUKSUUID+"="+p.RootMapperName,
			"rd.luks.uuid="+p.VarLUKSUUID,
			"rd.luks.name="+p.VarLUKSUUID+"=var",
		)

		// TPM2 auto-unlock (no PCR binding) when enabled.
		if p.TPM2 {
			cmdline = append(cmdline,
				"rd.luks.options="+p.RootLUKSUUID+"=tpm2-device=auto",
				"rd.luks.options="+p.VarLUKSUUID+"=tpm2-device=auto",
			)
		}

		// Boot (FAT32, never encrypted) and var (mapper device) mounts.
		cmdline = append(cmdline,
			"systemd.mount-extra=UUID="+p.BootUUID+":/boot:vfat:defaults",
			"systemd.mount-extra=/dev/mapper/var:/var:"+fsType+":defaults",
			"rd.etc.overlay=1",
			"rd.etc.overlay.var=/dev/mapper/var",
		)
	} else {
		cmdline = append(cmdline,
			"root=UUID="+p.RootUUID, "ro",
			"systemd.mount-extra=UUID="+p.BootUUID+":/boot:vfat:defaults",
			"systemd.mount-extra=UUID="+p.VarUUID+":/var:"+fsType+":defaults",
			"rd.etc.overlay=1",
			"rd.etc.overlay.var=UUID="+p.VarUUID,
		)
	}

	// HACK: Disable NVMe multipath so nvme0/nvme1 device naming stays stable
	// across reboots (CONFIG_NVME_MULTIPATH=y otherwise enumerates them
	// non-deterministically).
	cmdline = append(cmdline, "nvme_core.multipath=N")

	// User-supplied arguments last.
	cmdline = append(cmdline, p.ExtraArgs...)
	return cmdline
}
