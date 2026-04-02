package azureutil

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// KeyVaultName returns a deterministic, globally-unique Key Vault name derived
// from the full ARM resource group ID. The resource group ID is normalised to
// lowercase before hashing so the result is stable across case variations.
//
// Format: "cb-" + first 16 hex chars of SHA-256 = 19 chars total (within the
// 3-24 character Azure Key Vault name limit).
func KeyVaultName(resourceGroupID string) string {
	h := sha256.Sum256([]byte(strings.ToLower(resourceGroupID)))
	return fmt.Sprintf("cb-%x", h[:4]) // 4 bytes = 8 hex chars
}

// KeyVaultURL returns the HTTPS URL for the given Key Vault name.
func KeyVaultURL(kvName string) string {
	return fmt.Sprintf("https://%s.vault.azure.net", kvName)
}

// SSHPrivateKeySecretName is the name used for the SSH private key secret
// stored in each codebox's Key Vault.
const SSHPrivateKeySecretName = "ssh-private-key"
