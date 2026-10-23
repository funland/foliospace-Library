package launchprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMAMEDependencyKindUsesCatalogFlagsAndReferences(t *testing.T) {
	xml := `<mame build="0.288"><machine name="bios" isbios="yes"/><machine name="sound" isdevice="yes"/><machine name="support" runnable="no"/><machine name="orphan" runnable="no"/><machine name="parent"/><machine name="clone" cloneof="parent" romof="parent"><device_ref name="sound"/><device_ref name="support"/></machine><machine name="clone_only" cloneof="orphan"/><machine name="bios_game" romof="bios"/></mame>`
	filePath := filepath.Join(t.TempDir(), "list.xml")
	if err := os.WriteFile(filePath, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseMAMEListXMLFile(filePath, []string{"clone", "clone_only", "bios_game", "orphan"})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"bios": "bios", "sound": "device", "support": "support", "orphan": "", "parent": "", "clone": "", "clone_only": "", "bios_game": "", "missing": ""} {
		if got := catalog.DependencyKind(name); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
}
