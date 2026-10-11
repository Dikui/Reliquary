package chatgpt

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

type identityClaims struct {
	jwt.RegisteredClaims
	Nonce           string `json:"nonce"`
	Email           string `json:"email"`
	AuthorizedParty string `json:"azp"`
}

type jsonWebKey struct {
	KID string `json:"kid"`
	KTY string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	CRV string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (key jsonWebKey) publicKey() (any, error) {
	decode := base64.RawURLEncoding.DecodeString
	switch key.KTY {
	case "RSA":
		n, err := decode(key.N)
		if err != nil || len(n) < 256 {
			return nil, ErrInvalidIdentity
		}
		e, err := decode(key.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			return nil, ErrInvalidIdentity
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent*256 + int(b)
		}
		if exponent < 3 || exponent%2 == 0 {
			return nil, ErrInvalidIdentity
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}, nil
	case "EC":
		var curve elliptic.Curve
		switch key.CRV {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, ErrInvalidIdentity
		}
		x, err := decode(key.X)
		if err != nil {
			return nil, ErrInvalidIdentity
		}
		y, err := decode(key.Y)
		if err != nil {
			return nil, ErrInvalidIdentity
		}
		pointX, pointY := new(big.Int).SetBytes(x), new(big.Int).SetBytes(y)
		if !curve.IsOnCurve(pointX, pointY) {
			return nil, ErrInvalidIdentity
		}
		return &ecdsa.PublicKey{Curve: curve, X: pointX, Y: pointY}, nil
	case "OKP":
		x, err := decode(key.X)
		if key.CRV != "Ed25519" || err != nil || len(x) != ed25519.PublicKeySize {
			return nil, ErrInvalidIdentity
		}
		return ed25519.PublicKey(x), nil
	default:
		return nil, ErrInvalidIdentity
	}
}

func (m *Manager) validateIdentity(ctx context.Context, raw, clientID, nonce string) (*identityClaims, error) {
	if raw == "" {
		return nil, ErrInvalidIdentity
	}
	discovery, err := m.discovery(ctx)
	if err != nil {
		return nil, err
	}
	body, status, requestID, err := m.request(ctx, http.MethodGet, discovery.JWKSURI, nil, "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, endpointError(body, status, requestID)
	}
	var keys struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if json.Unmarshal(body, &keys) != nil {
		return nil, ErrInvalidIdentity
	}
	claims := &identityClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, ErrInvalidIdentity
		}
		for _, key := range keys.Keys {
			if key.KID == kid && (key.Use == "" || key.Use == "sig") && (key.Alg == "" || key.Alg == token.Method.Alg()) {
				return key.publicKey()
			}
		}
		return nil, ErrInvalidIdentity
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}), jwt.WithIssuer(m.endpoints.Issuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired(), jwt.WithTimeFunc(m.now))
	if err != nil || !parsed.Valid || claims.Subject == "" {
		return nil, ErrInvalidIdentity
	}
	if nonce != "" && subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 {
		return nil, ErrInvalidIdentity
	}
	// OIDC requires azp for multiple audiences, and any azp must be this client.
	if (len(claims.Audience) > 1 && claims.AuthorizedParty == "") || (claims.AuthorizedParty != "" && claims.AuthorizedParty != clientID) {
		return nil, ErrInvalidIdentity
	}
	return claims, nil
}
