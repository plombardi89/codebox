package azureutil_test

import (
	"strings"
	"testing"

	"github.com/plombardi89/codebox/internal/azureutil"
)

func TestKeyVaultName_Deterministic(t *testing.T) {
	rgID := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/codebox-mybox"

	name1 := azureutil.KeyVaultName(rgID)
	name2 := azureutil.KeyVaultName(rgID)

	if name1 != name2 {
		t.Errorf("KeyVaultName() not deterministic: %q != %q", name1, name2)
	}
}

func TestKeyVaultName_CaseInsensitive(t *testing.T) {
	lower := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/codebox-mybox"
	upper := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/CODEBOX-MYBOX"

	if azureutil.KeyVaultName(lower) != azureutil.KeyVaultName(upper) {
		t.Error("KeyVaultName() should be case-insensitive")
	}
}

func TestKeyVaultName_Format(t *testing.T) {
	rgID := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/codebox-mybox"
	name := azureutil.KeyVaultName(rgID)

	if !strings.HasPrefix(name, "cb-") {
		t.Errorf("KeyVaultName() = %q, want prefix %q", name, "cb-")
	}

	// Total length should be 3 (prefix) + 8 (hex) = 11.
	if len(name) != 11 {
		t.Errorf("KeyVaultName() length = %d, want 11", len(name))
	}

	// Must be within Azure Key Vault name limits (3-24 chars).
	if len(name) < 3 || len(name) > 24 {
		t.Errorf("KeyVaultName() length %d outside Azure limit [3, 24]", len(name))
	}
}

func TestKeyVaultName_DifferentInputsDifferentNames(t *testing.T) {
	name1 := azureutil.KeyVaultName("/subscriptions/sub1/resourceGroups/codebox-a")
	name2 := azureutil.KeyVaultName("/subscriptions/sub1/resourceGroups/codebox-b")

	if name1 == name2 {
		t.Errorf("different inputs should produce different names: both got %q", name1)
	}
}

func TestKeyVaultURL(t *testing.T) {
	url := azureutil.KeyVaultURL("cb-abc123")

	want := "https://cb-abc123.vault.azure.net"
	if url != want {
		t.Errorf("KeyVaultURL() = %q, want %q", url, want)
	}
}

func TestSSHPrivateKeySecretName(t *testing.T) {
	if azureutil.SSHPrivateKeySecretName == "" {
		t.Error("SSHPrivateKeySecretName should not be empty")
	}
}
