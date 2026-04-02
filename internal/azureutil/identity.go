package azureutil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// jwtClaims holds the subset of JWT claims we need.
type jwtClaims struct {
	OID      string `json:"oid"`
	TenantID string `json:"tid"`
	UPN      string `json:"upn"`
	Email    string `json:"email"`
}

// IdentityInfo contains the user's Azure AD identity details extracted from
// their credential's access token.
type IdentityInfo struct {
	ObjectID string
	TenantID string
	UPN      string // User Principal Name or email address
}

// IdentityFromCredential extracts the caller's Azure AD Object ID and Tenant
// ID by requesting an ARM token and decoding the JWT payload.
func IdentityFromCredential(ctx context.Context, cred azcore.TokenCredential) (*IdentityInfo, error) {
	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://management.azure.com/.default"},
	})
	if err != nil {
		return nil, fmt.Errorf("getting token: %w", err)
	}

	claims, err := decodeJWTClaims(token.Token)
	if err != nil {
		return nil, fmt.Errorf("decoding JWT: %w", err)
	}

	if claims.OID == "" {
		return nil, fmt.Errorf("JWT does not contain an oid claim")
	}

	if claims.TenantID == "" {
		return nil, fmt.Errorf("JWT does not contain a tid claim")
	}

	return &IdentityInfo{
		ObjectID: claims.OID,
		TenantID: claims.TenantID,
		UPN:      userPrincipalName(claims),
	}, nil
}

// userPrincipalName returns the user's principal name from JWT claims,
// preferring the upn claim and falling back to email.
func userPrincipalName(claims *jwtClaims) string {
	if claims.UPN != "" {
		return claims.UPN
	}

	return claims.Email
}

// IsMicrosoftUser reports whether the identity belongs to a @microsoft.com
// user, based on the UPN or email address in the JWT.
func IsMicrosoftUser(info *IdentityInfo) bool {
	return strings.HasSuffix(strings.ToLower(info.UPN), "@microsoft.com")
}

// decodeJWTClaims splits a JWT into its three parts and base64-decodes the
// payload (middle segment) without verifying the signature.
func decodeJWTClaims(token string) (*jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected 3 JWT segments, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("base64-decoding JWT payload: %w", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshaling JWT claims: %w", err)
	}

	return &claims, nil
}
