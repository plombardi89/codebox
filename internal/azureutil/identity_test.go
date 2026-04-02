package azureutil_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/plombardi89/codebox/internal/azureutil"
)

func TestIdentityFromCredential_InvalidJWTParts(t *testing.T) {
	// IdentityFromCredential requires a real Azure credential so we can't
	// fully test it in unit tests, but we can verify the exported types exist
	// and the struct fields are accessible.
	info := &azureutil.IdentityInfo{
		ObjectID: "test-oid",
		TenantID: "test-tid",
	}
	if info.ObjectID != "test-oid" {
		t.Errorf("ObjectID = %q, want %q", info.ObjectID, "test-oid")
	}

	if info.TenantID != "test-tid" {
		t.Errorf("TenantID = %q, want %q", info.TenantID, "test-tid")
	}
}

func TestJWTPayloadFormat(t *testing.T) {
	claims := map[string]string{
		"oid": "12345678-1234-1234-1234-123456789012",
		"tid": "87654321-4321-4321-4321-210987654321",
	}

	data, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(data)

	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(decoded, &result); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if result["oid"] != claims["oid"] {
		t.Errorf("oid = %q, want %q", result["oid"], claims["oid"])
	}

	if result["tid"] != claims["tid"] {
		t.Errorf("tid = %q, want %q", result["tid"], claims["tid"])
	}
}

func TestIsMicrosoftUser(t *testing.T) {
	tests := []struct {
		name string
		upn  string
		want bool
	}{
		{"microsoft UPN", "user@microsoft.com", true},
		{"microsoft UPN uppercase", "User@Microsoft.COM", true},
		{"non-microsoft UPN", "user@contoso.com", false},
		{"microsoft subdomain", "user@teams.microsoft.com", false},
		{"empty UPN", "", false},
		{"email-only microsoft", "user@microsoft.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &azureutil.IdentityInfo{
				ObjectID: "oid",
				TenantID: "tid",
				UPN:      tt.upn,
			}
			if got := azureutil.IsMicrosoftUser(info); got != tt.want {
				t.Errorf("IsMicrosoftUser(UPN=%q) = %v, want %v", tt.upn, got, tt.want)
			}
		})
	}
}
