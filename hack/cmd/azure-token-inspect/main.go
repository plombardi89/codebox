// azure-token-inspect is a debug tool that fetches an Azure ARM token using
// DefaultAzureCredential and prints the decoded JWT header, all claims, and
// the codebox-relevant derived fields.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/plombardi89/codebox/internal/azureutil"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("creating Azure credential: %w", err)
	}

	scope := "https://management.azure.com/.default"

	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{scope},
	})
	if err != nil {
		return fmt.Errorf("getting token for scope %s: %w", scope, err)
	}

	parts := strings.Split(token.Token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("expected 3 JWT segments, got %d", len(parts))
	}

	fmt.Println("=== Azure ARM Token ===")
	fmt.Println()

	// --- Header ---
	header, err := decodeSegment(parts[0])
	if err != nil {
		return fmt.Errorf("decoding JWT header: %w", err)
	}

	fmt.Println("--- Header ---")

	if err := printJSON(header); err != nil {
		return fmt.Errorf("printing header: %w", err)
	}

	fmt.Println()

	// --- Claims ---
	claims, err := decodeSegment(parts[1])
	if err != nil {
		return fmt.Errorf("decoding JWT payload: %w", err)
	}

	fmt.Println("--- Claims ---")

	if err := printJSON(claims); err != nil {
		return fmt.Errorf("printing claims: %w", err)
	}

	fmt.Println()

	// --- Codebox-Relevant Fields ---
	fmt.Println("--- Codebox-Relevant Fields ---")

	oid := stringClaim(claims, "oid")
	tid := stringClaim(claims, "tid")
	upn := stringClaim(claims, "upn")
	email := stringClaim(claims, "email")

	fmt.Printf("  Object ID (oid):  %s\n", valueOrMissing(oid))
	fmt.Printf("  Tenant ID (tid):  %s\n", valueOrMissing(tid))
	fmt.Printf("  UPN (upn):        %s\n", valueOrMissing(upn))
	fmt.Printf("  Email (email):    %s\n", valueOrMissing(email))

	// Derive the same fields codebox uses.
	effectiveUPN := upn
	if effectiveUPN == "" {
		effectiveUPN = email
	}

	info := &azureutil.IdentityInfo{
		ObjectID: oid,
		TenantID: tid,
		UPN:      effectiveUPN,
	}
	fmt.Printf("  IsMicrosoftUser:  %v\n", azureutil.IsMicrosoftUser(info))

	// Token times.
	if exp, ok := claims["exp"].(float64); ok {
		t := time.Unix(int64(exp), 0).UTC()
		fmt.Printf("  Expires (exp):    %s (%s from now)\n", t.Format(time.RFC3339), time.Until(t).Round(time.Second))
	}

	if iat, ok := claims["iat"].(float64); ok {
		t := time.Unix(int64(iat), 0).UTC()
		fmt.Printf("  Issued (iat):     %s\n", t.Format(time.RFC3339))
	}

	if aud, ok := claims["aud"].(string); ok {
		fmt.Printf("  Audience (aud):   %s\n", aud)
	}

	if iss, ok := claims["iss"].(string); ok {
		fmt.Printf("  Issuer (iss):     %s\n", iss)
	}

	return nil
}

// decodeSegment base64-decodes a JWT segment and unmarshals it as generic JSON.
func decodeSegment(seg string) (map[string]any, error) {
	data, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	return m, nil
}

// printJSON pretty-prints a map as indented JSON.
func printJSON(m map[string]any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return enc.Encode(m)
}

// stringClaim extracts a string claim from a JWT claims map, returning "" if
// the key is missing or not a string.
func stringClaim(claims map[string]any, key string) string {
	if s, ok := claims[key].(string); ok {
		return s
	}

	return ""
}

func valueOrMissing(v string) string {
	if v == "" {
		return "(not present)"
	}

	return v
}
