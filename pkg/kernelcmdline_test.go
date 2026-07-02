package pkg

import (
	"reflect"
	"strings"
	"testing"
)

func TestAssembleKernelCmdline_NonEncrypted(t *testing.T) {
	got := assembleKernelCmdline(kernelCmdlineParams{
		FilesystemType: "btrfs",
		RootUUID:       "ROOT",
		VarUUID:        "VAR",
		BootUUID:       "BOOT",
		ExtraArgs:      []string{"quiet", "splash"},
	})
	want := []string{
		"root=UUID=ROOT", "ro",
		"systemd.mount-extra=UUID=BOOT:/boot:vfat:defaults",
		"systemd.mount-extra=UUID=VAR:/var:btrfs:defaults",
		"rd.etc.overlay=1", "rd.etc.overlay.var=UUID=VAR",
		"nvme_core.multipath=N",
		"quiet", "splash",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestAssembleKernelCmdline_EncryptedWithTPM2(t *testing.T) {
	got := assembleKernelCmdline(kernelCmdlineParams{
		Encrypted:      true,
		TPM2:           true,
		FilesystemType: "ext4",
		RootMapperName: "root2",
		RootLUKSUUID:   "RL",
		VarLUKSUUID:    "VL",
		BootUUID:       "BOOT",
		ExtraArgs:      []string{"quiet"},
	})
	want := []string{
		"root=/dev/mapper/root2", "ro",
		"rd.luks.uuid=RL", "rd.luks.name=RL=root2",
		"rd.luks.uuid=VL", "rd.luks.name=VL=var",
		"rd.luks.options=RL=tpm2-device=auto", "rd.luks.options=VL=tpm2-device=auto",
		"systemd.mount-extra=UUID=BOOT:/boot:vfat:defaults",
		"systemd.mount-extra=/dev/mapper/var:/var:ext4:defaults",
		"rd.etc.overlay=1", "rd.etc.overlay.var=/dev/mapper/var",
		"nvme_core.multipath=N",
		"quiet",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestAssembleKernelCmdline_EncryptedNoTPM2OmitsOptions(t *testing.T) {
	got := assembleKernelCmdline(kernelCmdlineParams{
		Encrypted:      true,
		FilesystemType: "ext4",
		RootMapperName: "root1",
		RootLUKSUUID:   "RL",
		VarLUKSUUID:    "VL",
		BootUUID:       "BOOT",
	})
	for _, a := range got {
		if strings.Contains(a, "tpm2-device") {
			t.Errorf("no TPM2 options expected without TPM2, got %v", got)
		}
	}
}

func TestAssembleKernelCmdline_DefaultsFilesystemToExt4(t *testing.T) {
	got := assembleKernelCmdline(kernelCmdlineParams{
		RootUUID: "ROOT", VarUUID: "VAR", BootUUID: "BOOT",
	})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, ":/var:ext4:defaults") {
		t.Errorf("expected ext4 default for var mount, got %v", got)
	}
}

func TestUpdateRootSlot(t *testing.T) {
	tests := []struct {
		isTarget, root1Active bool
		wantMapper            string
		wantUseRoot2          bool
	}{
		// target = inactive slot
		{isTarget: true, root1Active: true, wantMapper: "root2", wantUseRoot2: true},
		{isTarget: true, root1Active: false, wantMapper: "root1", wantUseRoot2: false},
		// non-target = active slot
		{isTarget: false, root1Active: true, wantMapper: "root1", wantUseRoot2: false},
		{isTarget: false, root1Active: false, wantMapper: "root2", wantUseRoot2: true},
	}
	for _, tt := range tests {
		gotMapper, gotUseRoot2 := updateRootSlot(tt.isTarget, tt.root1Active)
		if gotMapper != tt.wantMapper || gotUseRoot2 != tt.wantUseRoot2 {
			t.Errorf("updateRootSlot(isTarget=%v, root1Active=%v) = (%q, %v), want (%q, %v)",
				tt.isTarget, tt.root1Active, gotMapper, gotUseRoot2, tt.wantMapper, tt.wantUseRoot2)
		}
	}
}

// TestAssembleKernelCmdline_InstallUpdateParity documents the whole point of the
// refactor: install (which hardcodes root1) and an update that targets root1
// must produce byte-identical cmdlines. The update side derives its root slot
// via updateRootSlot rather than hardcoding it, so this exercises that logic
// landing on root1 and matching the install output.
func TestAssembleKernelCmdline_InstallUpdateParity(t *testing.T) {
	const rootLUKS, varLUKS, boot = "R1LUKS", "VLUKS", "BOOT"
	extra := []string{"quiet", "splash"}

	// Install always targets root1.
	installCmdline := assembleKernelCmdline(kernelCmdlineParams{
		Encrypted: true, TPM2: true, FilesystemType: "btrfs",
		RootMapperName: "root1", RootLUKSUUID: rootLUKS, VarLUKSUUID: varLUKS,
		BootUUID: boot, ExtraArgs: extra,
	})

	// Update targeting the new root while root2 is currently active -> root1.
	mapper, useRoot2 := updateRootSlot(true /*isTarget*/, false /*root1Active*/)
	if useRoot2 || mapper != "root1" {
		t.Fatalf("precondition: expected update to target root1, got %q", mapper)
	}
	updateCmdline := assembleKernelCmdline(kernelCmdlineParams{
		Encrypted: true, TPM2: true, FilesystemType: "btrfs",
		RootMapperName: mapper, RootLUKSUUID: rootLUKS, VarLUKSUUID: varLUKS,
		BootUUID: boot, ExtraArgs: extra,
	})

	if !reflect.DeepEqual(installCmdline, updateCmdline) {
		t.Errorf("install and update cmdlines diverged:\ninstall %v\nupdate  %v", installCmdline, updateCmdline)
	}
}
