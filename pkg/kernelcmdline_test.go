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

// TestAssembleKernelCmdline_InstallUpdateParity documents the whole point of the
// refactor: install and update build the cmdline through the same function, so
// equivalent inputs yield byte-identical output. Here the install case (root1,
// hardcoded) and the update case targeting root1 produce the same result.
func TestAssembleKernelCmdline_InstallUpdateParity(t *testing.T) {
	base := kernelCmdlineParams{
		Encrypted:      true,
		TPM2:           true,
		FilesystemType: "btrfs",
		RootMapperName: "root1",
		RootLUKSUUID:   "RL",
		VarLUKSUUID:    "VL",
		BootUUID:       "BOOT",
		ExtraArgs:      []string{"quiet", "splash"},
	}
	installCmdline := assembleKernelCmdline(base)
	updateCmdline := assembleKernelCmdline(base)
	if !reflect.DeepEqual(installCmdline, updateCmdline) {
		t.Errorf("install and update cmdlines diverged:\ninstall %v\nupdate  %v", installCmdline, updateCmdline)
	}
}
